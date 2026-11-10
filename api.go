package flags

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
)

// Server is the HTTP API over a set of environments. Routes:
//
//	GET    /v1/envs/{env}/flags                    list flags
//	POST   /v1/envs/{env}/flags                    create a flag
//	GET    /v1/envs/{env}/flags/{key}              fetch a flag
//	PATCH  /v1/envs/{env}/flags/{key}              update enabled/rollout/rules
//	DELETE /v1/envs/{env}/flags/{key}              delete a flag
//	POST   /v1/envs/{env}/flags/{key}/evaluate     evaluate for a user context
type Server struct {
	resolve func(env string) (FlagStore, error)
	mux     *http.ServeMux
}

// NewServer serves the in-memory stores held by envs.
func NewServer(envs *Environments) *Server {
	return NewServerFunc(func(name string) (FlagStore, error) {
		st, err := envs.Env(name)
		if err != nil {
			return nil, err
		}
		return st, nil
	})
}

// NewServerFunc serves whatever store resolve returns for an environment name,
// allowing any FlagStore backend (e.g. SQLiteStore). resolve must return
// ErrUnknownEnvironment for names it doesn't know.
func NewServerFunc(resolve func(env string) (FlagStore, error)) *Server {
	s := &Server{resolve: resolve, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/envs/{env}/flags", s.handle(s.list))
	s.mux.HandleFunc("POST /v1/envs/{env}/flags", s.handle(s.create))
	s.mux.HandleFunc("GET /v1/envs/{env}/flags/{key}", s.handle(s.get))
	s.mux.HandleFunc("PATCH /v1/envs/{env}/flags/{key}", s.handle(s.update))
	s.mux.HandleFunc("DELETE /v1/envs/{env}/flags/{key}", s.handle(s.remove))
	s.mux.HandleFunc("POST /v1/envs/{env}/flags/{key}/evaluate", s.handle(s.evaluate))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// JSON wire types.
type ruleJSON struct {
	Attribute string   `json:"attribute"`
	Operator  Operator `json:"operator"`
	Values    []string `json:"values"`
}

func (r ruleJSON) rule() Rule {
	return Rule{Attribute: r.Attribute, Operator: r.Operator, Values: r.Values}
}

type flagJSON struct {
	Key     string     `json:"key"`
	Enabled bool       `json:"enabled"`
	Rollout int        `json:"rollout"`
	Rules   []ruleJSON `json:"rules"`
}

func toFlagJSON(f Flag) flagJSON {
	out := flagJSON{Key: f.Key, Enabled: f.Enabled, Rollout: f.Rollout, Rules: []ruleJSON{}}
	for _, r := range f.Rules {
		out.Rules = append(out.Rules, ruleJSON{r.Attribute, r.Operator, r.Values})
	}
	return out
}

type createRequest struct {
	Key     string     `json:"key"`
	Enabled bool       `json:"enabled"`
	Rollout *int       `json:"rollout"`
	Rules   []ruleJSON `json:"rules"`
}

type updateRequest struct {
	Enabled *bool       `json:"enabled"`
	Rollout *int        `json:"rollout"`
	Rules   *[]ruleJSON `json:"rules"` // when present, replaces all rules
}

type evaluateRequest struct {
	UserID     string            `json:"user_id"`
	Attributes map[string]string `json:"attributes"`
}

type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

type handlerFunc func(w http.ResponseWriter, r *http.Request, st FlagStore) error

// handle resolves the environment, runs h, and maps errors to JSON responses.
func (s *Server) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := s.resolve(r.PathValue("env"))
		if err == nil {
			err = h(w, r, st)
		}
		if err != nil {
			writeError(w, err)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		status = ae.status
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnknownEnvironment):
		status = http.StatusNotFound
	case errors.Is(err, ErrAlreadyExists):
		status = http.StatusConflict
	case errors.Is(err, ErrInvalidPercentage), errors.Is(err, ErrInvalidRule):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &apiError{http.StatusBadRequest, "invalid JSON body: " + err.Error()}
	}
	return nil
}

func validateRollout(p *int) error {
	if p != nil && (*p < 0 || *p > 100) {
		return ErrInvalidPercentage
	}
	return nil
}

func validateRules(rs []ruleJSON) error {
	for _, rj := range rs {
		if err := rj.rule().validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request, st FlagStore) error {
	fl := st.List()
	sort.Slice(fl, func(i, j int) bool { return fl[i].Key < fl[j].Key })
	out := make([]flagJSON, 0, len(fl))
	for _, f := range fl {
		out = append(out, toFlagJSON(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": out})
	return nil
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, st FlagStore) error {
	var req createRequest
	if err := decode(w, r, &req); err != nil {
		return err
	}
	if req.Key == "" {
		return &apiError{http.StatusBadRequest, "key is required"}
	}
	if err := validateRollout(req.Rollout); err != nil {
		return err
	}
	if err := validateRules(req.Rules); err != nil {
		return err
	}
	if err := st.Create(req.Key, req.Enabled); err != nil {
		return err
	}
	if req.Rollout != nil {
		if err := st.SetRollout(req.Key, *req.Rollout); err != nil {
			return err
		}
	}
	for _, rj := range req.Rules {
		if err := st.AddRule(req.Key, rj.rule()); err != nil {
			return err
		}
	}
	return s.respondFlag(w, st, req.Key, http.StatusCreated)
}

func (s *Server) respondFlag(w http.ResponseWriter, st FlagStore, key string, status int) error {
	f, err := st.Get(key)
	if err != nil {
		return err
	}
	writeJSON(w, status, toFlagJSON(f))
	return nil
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, st FlagStore) error {
	return s.respondFlag(w, st, r.PathValue("key"), http.StatusOK)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, st FlagStore) error {
	key := r.PathValue("key")
	var req updateRequest
	if err := decode(w, r, &req); err != nil {
		return err
	}
	if _, err := st.Get(key); err != nil {
		return err
	}
	// Validate everything up front so a bad request changes nothing.
	if err := validateRollout(req.Rollout); err != nil {
		return err
	}
	if req.Rules != nil {
		if err := validateRules(*req.Rules); err != nil {
			return err
		}
	}
	if req.Enabled != nil {
		if err := st.Set(key, *req.Enabled); err != nil {
			return err
		}
	}
	if req.Rollout != nil {
		if err := st.SetRollout(key, *req.Rollout); err != nil {
			return err
		}
	}
	if req.Rules != nil {
		if err := st.ClearRules(key); err != nil {
			return err
		}
		for _, rj := range *req.Rules {
			if err := st.AddRule(key, rj.rule()); err != nil {
				return err
			}
		}
	}
	return s.respondFlag(w, st, key, http.StatusOK)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request, st FlagStore) error {
	if err := st.Delete(r.PathValue("key")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request, st FlagStore) error {
	key := r.PathValue("key")
	var req evaluateRequest
	if err := decode(w, r, &req); err != nil {
		return err
	}
	if _, err := st.Get(key); err != nil {
		return err
	}
	on := st.Evaluate(key, Context{UserID: req.UserID, Attributes: req.Attributes})
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "enabled": on})
	return nil
}

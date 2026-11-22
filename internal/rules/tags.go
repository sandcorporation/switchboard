package rules

import "net/http"

// Service tags come with T3; until then no role carries a tag.

func (s *Service) tagsOf(q queryer, role string) ([]string, error) { return nil, nil }

func (s *Service) serviceTags(q queryer, svc string) ([]string, error) { return nil, nil }

func (s *Service) tagsAPI(w http.ResponseWriter, r *http.Request, c Caller, rest string) {
	http.NotFound(w, r)
}

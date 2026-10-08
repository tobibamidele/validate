package main

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var errEmailTaken = errors.New("email already registered")

type user struct {
	ID        uuid.UUID
	Email     string
	Name      string
	Bio       *string
	Age       int
	CreatedAt time.Time
}

// store is an in-memory stand-in for a database.
type store struct {
	mu    sync.Mutex
	users map[uuid.UUID]*user
}

func newStore() *store { return &store{users: map[uuid.UUID]*user{}} }

func (s *store) create(in CreateUser) (user, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Email == in.Email {
			return user{}, errEmailTaken
		}
	}
	u := &user{ID: uuid.New(), Email: in.Email, Age: in.Age, CreatedAt: time.Now().UTC()}
	s.users[u.ID] = u
	return *u, nil
}

func (s *store) get(id uuid.UUID) (user, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return user{}, false
	}
	return *u, true
}

func (s *store) list(search string, page, limit int) []user {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []user
	for _, u := range s.users {
		if search == "" || strings.Contains(u.Email, strings.ToLower(search)) || strings.Contains(strings.ToLower(u.Name), strings.ToLower(search)) {
			all = append(all, *u)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Email < all[j].Email })
	start := (page - 1) * limit
	if start >= len(all) {
		return nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

func (s *store) patch(id uuid.UUID, in PatchUser) (user, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return user{}, false
	}
	if name, set := in.Name.Value(); set {
		u.Name = name
	}
	if bio, set := in.Bio.Value(); set { // the field was sent
		if v, ok := bio.Value(); ok {
			u.Bio = &v // sent a value
		} else {
			u.Bio = nil // sent null: clear it
		}
	}
	return *u, true
}

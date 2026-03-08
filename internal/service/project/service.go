package project

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

var ErrInvalidInput = errors.New("invalid project input")

type Service struct {
	repo *repository.ProjectRepository
}

type CreateProjectInput struct {
	Name            string
	Path            string
	DefaultProvider string
	Model           string
	SystemPrompt    string
	FailurePolicy   string
}

type UpdateProjectAIInput struct {
	ProjectID       string
	DefaultProvider string
	Model           string
	SystemPrompt    string
	FailurePolicy   string
}

type UpdateProjectInput struct {
	ProjectID       string
	Name            string
	DefaultProvider string
	Model           string
	SystemPrompt    string
	FailurePolicy   string
}

func NewService(repo *repository.ProjectRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, in CreateProjectInput) (*domain.Project, error) {
	name := strings.TrimSpace(in.Name)
	path := strings.TrimSpace(in.Path)
	if name == "" || path == "" {
		return nil, ErrInvalidInput
	}
	defaultProvider, ok := normalizeProvider(in.DefaultProvider)
	if !ok {
		return nil, ErrInvalidInput
	}
	failurePolicy, ok := normalizeFailurePolicy(in.FailurePolicy)
	if !ok {
		return nil, ErrInvalidInput
	}

	now := time.Now().UTC()
	p := &domain.Project{
		ID:                  uuid.NewString(),
		Name:                name,
		Path:                filepath.Clean(path),
		DefaultProvider:     defaultProvider,
		Model:               strings.TrimSpace(in.Model),
		SystemPrompt:        strings.TrimSpace(in.SystemPrompt),
		FailurePolicy:       failurePolicy,
		AutoDispatchEnabled: false,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]domain.Project, error) {
	return s.repo.List(ctx, limit)
}

func (s *Service) UpdateAIConfig(ctx context.Context, in UpdateProjectAIInput) (*domain.Project, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return nil, ErrInvalidInput
	}
	defaultProvider, ok := normalizeProvider(in.DefaultProvider)
	if !ok {
		return nil, ErrInvalidInput
	}
	failurePolicy, ok := normalizeFailurePolicy(in.FailurePolicy)
	if !ok {
		return nil, ErrInvalidInput
	}
	if err := s.repo.UpdateAIConfig(ctx, projectID, defaultProvider, strings.TrimSpace(in.Model), strings.TrimSpace(in.SystemPrompt), failurePolicy); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, projectID)
}

func (s *Service) Update(ctx context.Context, in UpdateProjectInput) (*domain.Project, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	name := strings.TrimSpace(in.Name)
	if projectID == "" || name == "" {
		return nil, ErrInvalidInput
	}
	defaultProvider, ok := normalizeProvider(in.DefaultProvider)
	if !ok {
		return nil, ErrInvalidInput
	}
	failurePolicy, ok := normalizeFailurePolicy(in.FailurePolicy)
	if !ok {
		return nil, ErrInvalidInput
	}
	if err := s.repo.Update(ctx, projectID, name, defaultProvider, strings.TrimSpace(in.Model), strings.TrimSpace(in.SystemPrompt), failurePolicy); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, projectID)
}

func (s *Service) Delete(ctx context.Context, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ErrInvalidInput
	}
	return s.repo.DeleteWithRelatedData(ctx, projectID)
}

func normalizeProvider(raw string) (string, bool) {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return "claude", true
	}
	if p != "claude" && p != "codex" {
		return "", false
	}
	return p, true
}

func normalizeFailurePolicy(raw string) (domain.ProjectFailurePolicy, bool) {
	switch domain.ProjectFailurePolicy(strings.ToLower(strings.TrimSpace(raw))) {
	case "":
		return domain.ProjectFailurePolicyBlock, true
	case domain.ProjectFailurePolicyBlock, domain.ProjectFailurePolicyContinue:
		return domain.ProjectFailurePolicy(strings.ToLower(strings.TrimSpace(raw))), true
	default:
		return "", false
	}
}

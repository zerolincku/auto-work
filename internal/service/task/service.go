package task

import (
	"context"
	"errors"
	"strings"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

var (
	ErrInvalidStatus     = errors.New("invalid task status")
	ErrInvalidInput      = errors.New("invalid task input")
	ErrInvalidRetryState = errors.New("task status does not support retry")
	ErrTaskNotEditable   = errors.New("task is not editable while running")
	ErrTaskNotDeletable  = errors.New("task is not deletable while running")
)

type Service struct {
	repo        *repository.TaskRepository
	projectRepo *repository.ProjectRepository
}

type CreateTaskInput struct {
	ProjectID   string
	Title       string
	Description string
	Priority    int
	Provider    string
}

type UpdateTaskInput struct {
	TaskID      string
	Title       string
	Description string
	Priority    int
}

func NewService(repo *repository.TaskRepository, projectRepo *repository.ProjectRepository) *Service {
	return &Service{
		repo:        repo,
		projectRepo: projectRepo,
	}
}

func (s *Service) Create(ctx context.Context, in CreateTaskInput) (*domain.Task, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	title := strings.TrimSpace(in.Title)
	description := strings.TrimSpace(in.Description)
	if projectID == "" || title == "" || description == "" {
		return nil, ErrInvalidInput
	}
	if s.projectRepo != nil {
		if _, err := s.projectRepo.GetByID(ctx, projectID); err != nil {
			return nil, err
		}
	}
	if in.Priority < 0 {
		return nil, ErrInvalidInput
	}
	if in.Priority == 0 {
		priority, err := s.repo.NextAppendPriority(ctx, projectID, 100)
		if err != nil {
			return nil, err
		}
		in.Priority = priority
	}

	now := time.Now().UTC()
	t := &domain.Task{
		ProjectID:   projectID,
		Title:       title,
		Description: description,
		Priority:    in.Priority,
		Status:      domain.TaskPending,
		Provider:    "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) List(ctx context.Context, status string, provider string, projectID string, limit int) ([]domain.Task, error) {
	var st *domain.TaskStatus
	if strings.TrimSpace(status) != "" {
		parsed, ok := parseTaskStatus(status)
		if !ok {
			return nil, ErrInvalidStatus
		}
		st = &parsed
	}
	return s.repo.List(ctx, repository.TaskListFilter{
		Status:    st,
		Provider:  strings.TrimSpace(provider),
		ProjectID: strings.TrimSpace(projectID),
		Limit:     limit,
	})
}

func (s *Service) UpdateStatus(ctx context.Context, taskID string, status string) error {
	parsed, ok := parseTaskStatus(status)
	if !ok {
		return ErrInvalidStatus
	}
	return s.repo.UpdateStatus(ctx, taskID, parsed)
}

func (s *Service) Update(ctx context.Context, in UpdateTaskInput) (*domain.Task, error) {
	taskID := strings.TrimSpace(in.TaskID)
	title := strings.TrimSpace(in.Title)
	description := strings.TrimSpace(in.Description)
	if taskID == "" || title == "" || description == "" || in.Priority <= 0 {
		return nil, ErrInvalidInput
	}

	task, err := s.repo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status == domain.TaskRunning {
		return nil, ErrTaskNotEditable
	}

	task.Title = title
	task.Description = description
	task.Priority = in.Priority
	if err := s.repo.UpdateNonRunningTask(ctx, task); err != nil {
		if errors.Is(err, repository.ErrTaskNotEditable) {
			return nil, ErrTaskNotEditable
		}
		return nil, err
	}
	return s.repo.GetByID(ctx, taskID)
}

func (s *Service) Delete(ctx context.Context, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ErrInvalidInput
	}
	if err := s.repo.DeleteNonRunningTask(ctx, taskID); err != nil {
		if errors.Is(err, repository.ErrTaskNotDeletable) {
			return ErrTaskNotDeletable
		}
		return err
	}
	return nil
}

func (s *Service) Retry(ctx context.Context, taskID string) error {
	task, err := s.repo.GetByID(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return err
	}
	switch task.Status {
	case domain.TaskFailed, domain.TaskBlocked:
		return s.repo.ResetRetryToPending(ctx, task.ID)
	default:
		return ErrInvalidRetryState
	}
}

func parseTaskStatus(status string) (domain.TaskStatus, bool) {
	switch domain.TaskStatus(strings.TrimSpace(status)) {
	case domain.TaskPending, domain.TaskRunning, domain.TaskDone, domain.TaskFailed, domain.TaskBlocked:
		return domain.TaskStatus(strings.TrimSpace(status)), true
	default:
		return "", false
	}
}

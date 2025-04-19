package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/google/go-github/v71/github"
)

const (
	testOwner = "test-owner"
	testRepo  = "test-repo"
	testToken = "test-token"
)

func TestLoadConfig(t *testing.T) {
	// Set up test environment variables
	os.Setenv("GITHUB_TOKEN", testToken)
	os.Setenv("GITHUB_OWNER", testOwner)
	os.Setenv("GITHUB_REPO", testRepo)
	defer func() {
		os.Unsetenv("GITHUB_TOKEN")
		os.Unsetenv("GITHUB_OWNER")
		os.Unsetenv("GITHUB_REPO")
	}()

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := loadConfigWithFlags(flags, []string{})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cfg.token != testToken {
		t.Errorf("Expected token to be '%s', got '%s'", testToken, cfg.token)
	}
	if cfg.owner != testOwner {
		t.Errorf("Expected owner to be '%s', got '%s'", testOwner, cfg.owner)
	}
	if cfg.repo != testRepo {
		t.Errorf("Expected repo to be '%s', got '%s'", testRepo, cfg.repo)
	}
}

func TestLoadConfigWithFlags(t *testing.T) {
	// Clear environment variables
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GITHUB_OWNER")
	os.Unsetenv("GITHUB_REPO")

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	args := []string{
		"-token", "flag-token",
		"-owner", "flag-owner",
		"-repo", "flag-repo",
	}

	cfg, err := loadConfigWithFlags(flags, args)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cfg.token != "flag-token" {
		t.Errorf("Expected token to be 'flag-token', got '%s'", cfg.token)
	}
	if cfg.owner != "flag-owner" {
		t.Errorf("Expected owner to be 'flag-owner', got '%s'", cfg.owner)
	}
	if cfg.repo != "flag-repo" {
		t.Errorf("Expected repo to be 'flag-repo', got '%s'", cfg.repo)
	}
}

func TestGetRepositoryInfoFromHTTPS(t *testing.T) {
	// Mock git config command execution
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestGitConfigHelper", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"MOCK_GIT_OUTPUT=https://github.com/test-owner/test-repo.git",
		}
		return cmd
	}

	owner, repo, err := getRepositoryInfo()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if owner != "test-owner" {
		t.Errorf("Expected owner to be 'test-owner', got '%s'", owner)
	}
	if repo != "test-repo" {
		t.Errorf("Expected repo to be 'test-repo', got '%s'", repo)
	}
}

func TestGetRepositoryInfoFromSSH(t *testing.T) {
	// Mock git config command execution
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestGitConfigHelper", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"MOCK_GIT_OUTPUT=git@github.com:test-owner/test-repo.git",
		}
		return cmd
	}

	owner, repo, err := getRepositoryInfo()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if owner != "test-owner" {
		t.Errorf("Expected owner to be 'test-owner', got '%s'", owner)
	}
	if repo != "test-repo" {
		t.Errorf("Expected repo to be 'test-repo', got '%s'", repo)
	}
}

// TestGitConfigHelper is a helper for mocking git config command
func TestGitConfigHelper(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Println(os.Getenv("MOCK_GIT_OUTPUT"))
	os.Exit(0)
}

type mockTransport struct {
	// モックレスポンスを保持
	responses map[string]interface{}
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	key := req.URL.Path

	// POSTリクエストの場合、パスにメソッドを追加
	if req.Method == http.MethodPost {
		key = fmt.Sprintf("%s_%s", req.Method, req.URL.Path)
	}

	if response, ok := m.responses[key]; ok {
		recorder.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(recorder).Encode(response); err != nil {
			return nil, fmt.Errorf("failed to encode response: %v", err)
		}
	} else {
		http.Error(recorder, fmt.Sprintf("Not found: %s %s", req.Method, key), http.StatusNotFound)
	}

	return recorder.Result(), nil
}

func TestPRProcessor_ProcessPullRequests(t *testing.T) {
	ctx := context.Background()
	cfg := &config{
		token: "test-token",
		owner: "test-owner",
		repo:  "test-repo",
	}

	// モックのレスポンスを設定
	mockResp := &mockTransport{
		responses: map[string]interface{}{
			"/repos/test-owner/test-repo/pulls": []*github.PullRequest{
				{
					Number: github.Ptr(1),
					Title:  github.Ptr("Test PR"),
					Head: &github.PullRequestBranch{
						SHA: github.Ptr("test-sha"),
					},
					Base: &github.PullRequestBranch{
						SHA: github.Ptr("base-sha"),
					},
				},
			},
			"/repos/test-owner/test-repo/commits/test-sha/status": &github.CombinedStatus{
				State: github.Ptr("success"),
			},
			"/repos/test-owner/test-repo/pulls/1/merge": &github.PullRequestMergeResult{
				Merged:  github.Ptr(true),
				Message: github.Ptr("Pull Request successfully merged"),
			},
			"/repos/test-owner/test-repo/commits/base-sha...test-sha": &github.CommitsComparison{
				BehindBy: github.Ptr(0),
			},
		},
	}

	// モックのHTTPクライアントを作成
	httpClient := &http.Client{Transport: mockResp}
	client := github.NewClient(httpClient)

	processor := &PRProcessor{
		client: client,
		cfg:    cfg,
		ctx:    ctx,
	}

	err := processor.ProcessPullRequests()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

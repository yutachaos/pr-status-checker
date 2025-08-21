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
	"strings"
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
	if err := os.Setenv("GITHUB_TOKEN", testToken); err != nil {
		t.Fatalf("Failed to set GITHUB_TOKEN: %v", err)
	}
	if err := os.Setenv("GITHUB_OWNER", testOwner); err != nil {
		t.Fatalf("Failed to set GITHUB_OWNER: %v", err)
	}
	if err := os.Setenv("GITHUB_REPO", testRepo); err != nil {
		t.Fatalf("Failed to set GITHUB_REPO: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("GITHUB_TOKEN"); err != nil {
			t.Errorf("Failed to unset GITHUB_TOKEN: %v", err)
		}
		if err := os.Unsetenv("GITHUB_OWNER"); err != nil {
			t.Errorf("Failed to unset GITHUB_OWNER: %v", err)
		}
		if err := os.Unsetenv("GITHUB_REPO"); err != nil {
			t.Errorf("Failed to unset GITHUB_REPO: %v", err)
		}
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
	for _, env := range []string{"GITHUB_TOKEN", "GITHUB_OWNER", "GITHUB_REPO", "GITHUB_PR_SKIP_PATTERN", "GITHUB_PR_AUTHOR_PATTERN"} {
		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("Failed to unset %s: %v", env, err)
		}
	}

	testCases := []struct {
		name     string
		args     []string
		expected config
	}{
		{
			name: "with all flags",
			args: []string{
				"-token", "flag-token",
				"-owner", "flag-owner",
				"-repo", "flag-repo",
				"-approve=false",
				"-skip-pattern", "^WIP:",
				"-author-pattern", "^dependabot",
			},
			expected: config{
				token:         "flag-token",
				owner:         "flag-owner",
				repo:          "flag-repo",
				approve:       false,
				skipPattern:   "^WIP:",
				authorPattern: "^dependabot",
			},
		},
		{
			name: "default values",
			args: []string{
				"-token", "flag-token",
				"-owner", "flag-owner",
				"-repo", "flag-repo",
			},
			expected: config{
				token:         "flag-token",
				owner:         "flag-owner",
				repo:          "flag-repo",
				approve:       true,
				skipPattern:   "",
				authorPattern: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg, err := loadConfigWithFlags(flags, tc.args)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if cfg.token != tc.expected.token {
				t.Errorf("Expected token to be '%s', got '%s'", tc.expected.token, cfg.token)
			}
			if cfg.owner != tc.expected.owner {
				t.Errorf("Expected owner to be '%s', got '%s'", tc.expected.owner, cfg.owner)
			}
			if cfg.repo != tc.expected.repo {
				t.Errorf("Expected repo to be '%s', got '%s'", tc.expected.repo, cfg.repo)
			}
			if cfg.approve != tc.expected.approve {
				t.Errorf("Expected approve to be %v, got %v", tc.expected.approve, cfg.approve)
			}
			if cfg.skipPattern != tc.expected.skipPattern {
				t.Errorf("Expected skipPattern to be '%s', got '%s'", tc.expected.skipPattern, cfg.skipPattern)
			}
			if cfg.authorPattern != tc.expected.authorPattern {
				t.Errorf("Expected authorPattern to be '%s', got '%s'", tc.expected.authorPattern, cfg.authorPattern)
			}
		})
	}
}

func TestGetRepositoryInfoFromHTTPS(t *testing.T) {
	// Mock git config command execution
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestGitConfigHelper", "--", command}
		cs = append(cs, args...)
		//nolint:gosec // This is a test helper that only runs with specific test flags
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
		//nolint:gosec // This is a test helper that only runs with specific test flags
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

	// POSTリクエストの場合、レビューエンドポイントへのリクエストを特別に処理
	if req.Method == http.MethodPost && strings.HasSuffix(key, "/reviews") {
		response, ok := m.responses[key]
		if !ok {
			response = m.responses[strings.TrimSuffix(key, "/reviews")]
		}
		recorder.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(recorder).Encode(response); err != nil {
			return nil, fmt.Errorf("failed to encode response: %v", err)
		}
		return recorder.Result(), nil
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
	testCases := []struct {
		name          string
		approve       bool
		skipPattern   string
		authorPattern string
		prTitle       string
		prAuthor      string
		isDraft       bool
		shouldSkip    bool
	}{
		{
			name:     "with auto approve",
			approve:  true,
			prTitle:  "Test PR",
			prAuthor: "test-user",
		},
		{
			name:     "without auto approve",
			approve:  false,
			prTitle:  "Test PR",
			prAuthor: "test-user",
		},
		{
			name:        "skip WIP PR",
			approve:     true,
			skipPattern: "^WIP:",
			prTitle:     "WIP: Test PR",
			prAuthor:    "test-user",
			shouldSkip:  true,
		},
		{
			name:        "non-WIP PR",
			approve:     true,
			skipPattern: "^WIP:",
			prTitle:     "Test PR",
			prAuthor:    "test-user",
			shouldSkip:  false,
		},
		{
			name:       "draft PR",
			approve:    true,
			prTitle:    "Draft PR",
			prAuthor:   "test-user",
			isDraft:    true,
			shouldSkip: true,
		},
		{
			name:          "author filter match",
			approve:       true,
			authorPattern: "^dependabot",
			prTitle:       "Bump dependency",
			prAuthor:      "dependabot[bot]",
			shouldSkip:    false,
		},
		{
			name:          "author filter no match",
			approve:       true,
			authorPattern: "^dependabot",
			prTitle:       "Test PR",
			prAuthor:      "test-user",
			shouldSkip:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:         "test-token",
				owner:         "test-owner",
				repo:          "test-repo",
				approve:       tc.approve,
				skipPattern:   tc.skipPattern,
				authorPattern: tc.authorPattern,
			}

			// Set up mock responses
			mockResp := &mockTransport{
				responses: map[string]interface{}{
					"/repos/test-owner/test-repo/pulls": []*github.PullRequest{
						{
							Number: github.Ptr(1),
							Title:  github.Ptr(tc.prTitle),
							Draft:  github.Ptr(tc.isDraft),
							User: &github.User{
								Login: github.Ptr(tc.prAuthor),
							},
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
					"/repos/test-owner/test-repo/pulls/1/reviews": &github.PullRequestReview{
						ID:    github.Ptr[int64](123),
						State: github.Ptr("APPROVED"),
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

			// Create mock HTTP client
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
		})
	}
}

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
				token:           "flag-token",
				owner:           "flag-owner",
				repo:            "flag-repo",
				approve:         false,
				skipPattern:     "^WIP:",
				authorPattern:   "^dependabot",
				filterByReviewer: true,
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
				token:           "flag-token",
				owner:           "flag-owner",
				repo:            "flag-repo",
				approve:         true,
				skipPattern:     "",
				authorPattern:   "",
				filterByReviewer: true,
			},
		},
		{
			name: "with no-filter-reviewer flag",
			args: []string{
				"-token", "flag-token",
				"-owner", "flag-owner",
				"-repo", "flag-repo",
				"-no-filter-reviewer",
			},
			expected: config{
				token:           "flag-token",
				owner:           "flag-owner",
				repo:            "flag-repo",
				approve:         true,
				skipPattern:     "",
				authorPattern:   "",
				filterByReviewer: false,
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
			if cfg.filterByReviewer != tc.expected.filterByReviewer {
				t.Errorf("Expected filterByReviewer to be %v, got %v", tc.expected.filterByReviewer, cfg.filterByReviewer)
			}
		})
	}
}

func TestLoadConfigWithReviewerFilterEnvVar(t *testing.T) {
	// Clear environment variables
	for _, env := range []string{"GITHUB_TOKEN", "GITHUB_OWNER", "GITHUB_REPO", "GITHUB_NO_FILTER_REVIEWER"} {
		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("Failed to unset %s: %v", env, err)
		}
	}
	defer func() {
		if err := os.Unsetenv("GITHUB_NO_FILTER_REVIEWER"); err != nil {
			t.Errorf("Failed to unset GITHUB_NO_FILTER_REVIEWER: %v", err)
		}
	}()

	testCases := []struct {
		name           string
		envValue       string
		expectedFilter bool
	}{
		{
			name:           "GITHUB_NO_FILTER_REVIEWER=true disables filter",
			envValue:       "true",
			expectedFilter: false,
		},
		{
			name:           "GITHUB_NO_FILTER_REVIEWER=1 disables filter",
			envValue:       "1",
			expectedFilter: false,
		},
		{
			name:           "GITHUB_NO_FILTER_REVIEWER=false keeps default",
			envValue:       "false",
			expectedFilter: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up environment variables first
			_ = os.Unsetenv("GITHUB_NO_FILTER_REVIEWER")
			_ = os.Unsetenv("GITHUB_TOKEN")
			_ = os.Unsetenv("GITHUB_OWNER")
			_ = os.Unsetenv("GITHUB_REPO")

			if err := os.Setenv("GITHUB_NO_FILTER_REVIEWER", tc.envValue); err != nil {
				t.Fatalf("Failed to set GITHUB_NO_FILTER_REVIEWER: %v", err)
			}
			if err := os.Setenv("GITHUB_TOKEN", "test-token"); err != nil {
				t.Fatalf("Failed to set GITHUB_TOKEN: %v", err)
			}
			if err := os.Setenv("GITHUB_OWNER", "test-owner"); err != nil {
				t.Fatalf("Failed to set GITHUB_OWNER: %v", err)
			}
			if err := os.Setenv("GITHUB_REPO", "test-repo"); err != nil {
				t.Fatalf("Failed to set GITHUB_REPO: %v", err)
			}

			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg, err := loadConfigWithFlags(flags, []string{})
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if cfg.filterByReviewer != tc.expectedFilter {
				t.Errorf("Expected filterByReviewer to be %v, got %v", tc.expectedFilter, cfg.filterByReviewer)
			}

			// Clean up
			_ = os.Unsetenv("GITHUB_NO_FILTER_REVIEWER")
			_ = os.Unsetenv("GITHUB_TOKEN")
			_ = os.Unsetenv("GITHUB_OWNER")
			_ = os.Unsetenv("GITHUB_REPO")
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

	// Handle GET /user endpoint for getting current user
	if req.Method == http.MethodGet && key == "/user" {
		response, ok := m.responses[key]
		if !ok {
			response = &github.User{
				Login: github.Ptr("test-reviewer"),
			}
		}
		recorder.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(recorder).Encode(response); err != nil {
			return nil, fmt.Errorf("failed to encode response: %v", err)
		}
		return recorder.Result(), nil
	}

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
		name            string
		approve         bool
		skipPattern     string
		authorPattern   string
		filterByReviewer bool
		prTitle         string
		prAuthor        string
		isDraft         bool
		requestedReviewers []string
		shouldSkip      bool
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
		{
			name:              "reviewer filter enabled - user is reviewer",
			approve:           true,
			filterByReviewer:  true,
			prTitle:           "Test PR",
			prAuthor:          "test-user",
			requestedReviewers: []string{"test-reviewer"},
			shouldSkip:        false,
		},
		{
			name:              "reviewer filter enabled - user is not reviewer",
			approve:           true,
			filterByReviewer:  true,
			prTitle:           "Test PR",
			prAuthor:          "test-user",
			requestedReviewers: []string{"other-reviewer"},
			shouldSkip:        true,
		},
		{
			name:              "reviewer filter enabled - no reviewers",
			approve:           true,
			filterByReviewer:  true,
			prTitle:           "Test PR",
			prAuthor:          "test-user",
			requestedReviewers: []string{},
			shouldSkip:        true,
		},
		{
			name:              "reviewer filter disabled - process all PRs",
			approve:           true,
			filterByReviewer:  false,
			prTitle:           "Test PR",
			prAuthor:          "test-user",
			requestedReviewers: []string{"other-reviewer"},
			shouldSkip:        false,
		},
	}

		for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:           "test-token",
				owner:           "test-owner",
				repo:            "test-repo",
				approve:         tc.approve,
				skipPattern:     tc.skipPattern,
				authorPattern:   tc.authorPattern,
				filterByReviewer: tc.filterByReviewer,
			}

			// Build requested reviewers list
			var reviewers []*github.User
			for _, reviewerLogin := range tc.requestedReviewers {
				reviewers = append(reviewers, &github.User{
					Login: github.Ptr(reviewerLogin),
				})
			}

			// Set up mock responses
			mockResp := &mockTransport{
				responses: map[string]interface{}{
					"/user": &github.User{
						Login: github.Ptr("test-reviewer"),
					},
					"/repos/test-owner/test-repo/pulls": []*github.PullRequest{
						{
							Number:            github.Ptr(1),
							Title:             github.Ptr(tc.prTitle),
							Draft:             github.Ptr(tc.isDraft),
							RequestedReviewers: reviewers,
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

			// Get current user if filterByReviewer is enabled
			currentUser := ""
			if cfg.filterByReviewer {
				user, _, err := client.Users.Get(ctx, "")
				if err != nil {
					t.Fatalf("Failed to get current user: %v", err)
				}
				currentUser = user.GetLogin()
			}

			processor := &PRProcessor{
				client:      client,
				cfg:         cfg,
				ctx:         ctx,
				currentUser: currentUser,
			}

			err := processor.ProcessPullRequests()
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

func TestShouldSkipPR_ReviewerFilter(t *testing.T) {
	testCases := []struct {
		name              string
		filterByReviewer  bool
		currentUser       string
		requestedReviewers []string
		expectedSkip      bool
	}{
		{
			name:              "filter enabled - user is reviewer",
			filterByReviewer:  true,
			currentUser:       "test-reviewer",
			requestedReviewers: []string{"test-reviewer", "other-reviewer"},
			expectedSkip:      false,
		},
		{
			name:              "filter enabled - user is not reviewer",
			filterByReviewer:  true,
			currentUser:       "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			expectedSkip:      true,
		},
		{
			name:              "filter enabled - no reviewers",
			filterByReviewer:  true,
			currentUser:       "test-reviewer",
			requestedReviewers: []string{},
			expectedSkip:      true,
		},
		{
			name:              "filter disabled - process all",
			filterByReviewer:  false,
			currentUser:       "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			expectedSkip:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				filterByReviewer: tc.filterByReviewer,
			}

			// Build requested reviewers list
			var reviewers []*github.User
			for _, reviewerLogin := range tc.requestedReviewers {
				reviewers = append(reviewers, &github.User{
					Login: github.Ptr(reviewerLogin),
				})
			}

			pr := &github.PullRequest{
				Number:            github.Ptr(1),
				Title:             github.Ptr("Test PR"),
				RequestedReviewers: reviewers,
			}

			processor := &PRProcessor{
				client:      nil, // Not needed for this test
				cfg:         cfg,
				ctx:         ctx,
				currentUser: tc.currentUser,
			}

			shouldSkip, err := processor.shouldSkipPR(pr)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if shouldSkip != tc.expectedSkip {
				t.Errorf("Expected shouldSkip to be %v, got %v", tc.expectedSkip, shouldSkip)
			}
		})
	}
}

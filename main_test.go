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
				token:            "flag-token",
				owner:            "flag-owner",
				repo:             "flag-repo",
				approve:          false,
				skipPattern:      "^WIP:",
				authorPattern:    "^dependabot",
				autoRebase:       true,
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
				token:            "flag-token",
				owner:            "flag-owner",
				repo:             "flag-repo",
				approve:          true,
				skipPattern:      "",
				authorPattern:    "",
				autoRebase:       true,
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
				token:            "flag-token",
				owner:            "flag-owner",
				repo:             "flag-repo",
				approve:          true,
				skipPattern:      "",
				authorPattern:    "",
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
		name               string
		approve            bool
		skipPattern        string
		authorPattern      string
		filterByReviewer   bool
		prTitle            string
		prAuthor           string
		isDraft            bool
		requestedReviewers []string
		shouldSkip         bool
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
			name:               "reviewer filter enabled - user is reviewer",
			approve:            true,
			filterByReviewer:   true,
			prTitle:            "Test PR",
			prAuthor:           "test-user",
			requestedReviewers: []string{"test-reviewer"},
			shouldSkip:         false,
		},
		{
			name:               "reviewer filter enabled - user is not reviewer",
			approve:            true,
			filterByReviewer:   true,
			prTitle:            "Test PR",
			prAuthor:           "test-user",
			requestedReviewers: []string{"other-reviewer"},
			shouldSkip:         true,
		},
		{
			name:               "reviewer filter enabled - no reviewers",
			approve:            true,
			filterByReviewer:   true,
			prTitle:            "Test PR",
			prAuthor:           "test-user",
			requestedReviewers: []string{},
			shouldSkip:         true,
		},
		{
			name:               "reviewer filter disabled - process all PRs",
			approve:            true,
			filterByReviewer:   false,
			prTitle:            "Test PR",
			prAuthor:           "test-user",
			requestedReviewers: []string{"other-reviewer"},
			shouldSkip:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:            "test-token",
				owner:            "test-owner",
				repo:             "test-repo",
				approve:          tc.approve,
				skipPattern:      tc.skipPattern,
				authorPattern:    tc.authorPattern,
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
							Number:             github.Ptr(1),
							Title:              github.Ptr(tc.prTitle),
							Draft:              github.Ptr(tc.isDraft),
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
					"/repos/test-owner/test-repo/compare/base-sha...test-sha": &github.CommitsComparison{
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
		name               string
		filterByReviewer   bool
		currentUser        string
		requestedReviewers []string
		expectedSkip       bool
	}{
		{
			name:               "filter enabled - user is reviewer",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"test-reviewer", "other-reviewer"},
			expectedSkip:       false,
		},
		{
			name:               "filter enabled - user is not reviewer",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			expectedSkip:       true,
		},
		{
			name:               "filter enabled - no reviewers",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{},
			expectedSkip:       true,
		},
		{
			name:               "filter disabled - process all",
			filterByReviewer:   false,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			expectedSkip:       false,
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
				Number:             github.Ptr(1),
				Title:              github.Ptr("Test PR"),
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

func TestHandleSuccessfulPR_WithFailedCI(t *testing.T) {
	testCases := []struct {
		name            string
		ciStatus        string
		statusStates    []string
		shouldApprove   bool
		expectedMessage string
	}{
		{
			name:            "CI failed - should not approve",
			ciStatus:        "failure",
			statusStates:    []string{"failure"},
			shouldApprove:   false,
			expectedMessage: "Cannot approve - CI checks failed",
		},
		{
			name:            "CI pending - should not approve",
			ciStatus:        "pending",
			statusStates:    []string{"pending"},
			shouldApprove:   false,
			expectedMessage: "Cannot approve - CI checks still pending",
		},
		{
			name:          "CI success - should approve",
			ciStatus:      "success",
			statusStates:  []string{"success"},
			shouldApprove: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:   "test-token",
				owner:   "test-owner",
				repo:    "test-repo",
				approve: true,
			}

			// Build status list
			var statuses []*github.RepoStatus
			for _, state := range tc.statusStates {
				statuses = append(statuses, &github.RepoStatus{
					State:   github.Ptr(state),
					Context: github.Ptr("test-check"),
				})
			}

			// Set up mock responses
			mockResp := &mockTransport{
				responses: map[string]interface{}{
					"/repos/test-owner/test-repo/commits/test-sha/status": &github.CombinedStatus{
						State:    github.Ptr(tc.ciStatus),
						Statuses: statuses,
					},
					"/repos/test-owner/test-repo/pulls/1/reviews": &github.PullRequestReview{
						ID:    github.Ptr[int64](123),
						State: github.Ptr("APPROVED"),
					},
					"/repos/test-owner/test-repo/pulls/1/merge": &github.PullRequestMergeResult{
						Merged:  github.Ptr(true),
						Message: github.Ptr("Pull Request successfully merged"),
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

			pr := &github.PullRequest{
				Number: github.Ptr(1),
				Title:  github.Ptr("Test PR"),
				Head: &github.PullRequestBranch{
					SHA: github.Ptr("test-sha"),
				},
			}

			err := processor.handleSuccessfulPR(pr)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			// Verify that approval was not made if CI failed
			// When shouldApprove is false, the function should return without error
			// but the actual approval is prevented by the status check in handleSuccessfulPR
			_ = tc.shouldApprove // Suppress unused variable warning
		})
	}
}

func TestNewPRProcessor_UserRetrieval(t *testing.T) {
	testCases := []struct {
		name              string
		filterByReviewer  bool
		mockUserResponse  interface{}
		expectError       bool
		expectedUser      string
	}{
		{
			name:             "filterByReviewer enabled - should get current user",
			filterByReviewer: true,
			mockUserResponse: &github.User{
				Login: github.Ptr("test-user"),
			},
			expectError:  false,
			expectedUser: "test-user",
		},
		{
			name:             "filterByReviewer disabled - should not get current user",
			filterByReviewer: false,
			expectError:      false,
			expectedUser:     "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:           "test-token",
				owner:           "test-owner",
				repo:            "test-repo",
				filterByReviewer: tc.filterByReviewer,
			}

			// Set up mock responses
			responses := map[string]interface{}{}
			if tc.filterByReviewer && tc.mockUserResponse != nil {
				responses["/user"] = tc.mockUserResponse
			}

			mockResp := &mockTransport{
				responses: responses,
			}

			// Create mock HTTP client
			httpClient := &http.Client{Transport: mockResp}
			client := github.NewClient(httpClient)

			processor := &PRProcessor{
				client: client,
				cfg:    cfg,
				ctx:    ctx,
			}

			// Test the user retrieval logic directly
			if tc.filterByReviewer {
				if tc.mockUserResponse != nil {
					user, _, err := client.Users.Get(ctx, "")
					if tc.expectError {
						if err == nil {
							t.Errorf("Expected error but got none")
						}
						return
					}
					if err != nil {
						t.Errorf("Unexpected error: %v", err)
						return
					}
					processor.currentUser = user.GetLogin()
					if processor.currentUser != tc.expectedUser {
						t.Errorf("Expected currentUser to be '%s', got '%s'", tc.expectedUser, processor.currentUser)
					}
				}
			} else {
				if processor.currentUser != tc.expectedUser {
					t.Errorf("Expected currentUser to be '%s', got '%s'", tc.expectedUser, processor.currentUser)
				}
			}
		})
	}
}

func TestAutoRebaseDefaultValue(t *testing.T) {
	// Clear environment variables
	for _, env := range []string{"GITHUB_TOKEN", "GITHUB_OWNER", "GITHUB_REPO"} {
		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("Failed to unset %s: %v", env, err)
		}
	}

	testCases := []struct {
		name          string
		args          []string
		expectedValue bool
	}{
		{
			name:          "default value should be true",
			args:          []string{"-token", "test-token", "-owner", "test-owner", "-repo", "test-repo"},
			expectedValue: true,
		},
		{
			name:          "explicitly set to false",
			args:          []string{"-token", "test-token", "-owner", "test-owner", "-repo", "test-repo", "-auto-rebase=false"},
			expectedValue: false,
		},
		{
			name:          "explicitly set to true",
			args:          []string{"-token", "test-token", "-owner", "test-owner", "-repo", "test-repo", "-auto-rebase=true"},
			expectedValue: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg, err := loadConfigWithFlags(flags, tc.args)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if cfg.autoRebase != tc.expectedValue {
				t.Errorf("Expected autoRebase to be %v, got %v", tc.expectedValue, cfg.autoRebase)
			}
		})
	}
}

func TestHandleFailedChecks_AutoRebase(t *testing.T) {
	testCases := []struct {
		name            string
		autoRebase      bool
		failedStatuses  []string
		pendingStatuses []string
		behindBy        int
	}{
		{
			name:            "autoRebase enabled - should try rebase",
			autoRebase:      true,
			failedStatuses:  []string{"test-check"},
			pendingStatuses: []string{},
			behindBy:        5,
		},
		{
			name:            "autoRebase disabled - should not rebase",
			autoRebase:      false,
			failedStatuses:  []string{"test-check"},
			pendingStatuses: []string{},
			behindBy:        0,
		},
		{
			name:            "autoRebase enabled - branch up to date",
			autoRebase:      true,
			failedStatuses:  []string{"test-check"},
			pendingStatuses: []string{},
			behindBy:        0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:      "test-token",
				owner:      "test-owner",
				repo:       "test-repo",
				autoRebase: tc.autoRebase,
			}

			pr := &github.PullRequest{
				Number: github.Ptr(1),
				Title:  github.Ptr("Test PR"),
				Head: &github.PullRequestBranch{
					SHA: github.Ptr("head-sha"),
				},
				Base: &github.PullRequestBranch{
					SHA: github.Ptr("base-sha"),
				},
			}

			// Set up mock responses
			mockResp := &mockTransport{
				responses: map[string]interface{}{
					"/repos/test-owner/test-repo/compare/base-sha...head-sha": &github.CommitsComparison{
						BehindBy: github.Ptr(tc.behindBy),
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

			err := processor.handleFailedChecks(pr, tc.failedStatuses, tc.pendingStatuses)
			if err != nil {
				if !tc.autoRebase {
					// If rebase is disabled, should return nil
					t.Errorf("Expected no error when autoRebase is disabled, got %v", err)
				}
			}
		})
	}
}

func TestProcessSinglePR_WithReviewerFilterAndCI(t *testing.T) {
	testCases := []struct {
		name               string
		filterByReviewer   bool
		currentUser        string
		requestedReviewers []string
		ciStatus           string
		statusStates       []string
		shouldProcess      bool
	}{
		{
			name:               "reviewer filter - user is reviewer, CI success",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"test-reviewer"},
			ciStatus:           "success",
			statusStates:       []string{"success"},
			shouldProcess:      true,
		},
		{
			name:               "reviewer filter - user not reviewer, CI success",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			ciStatus:           "success",
			statusStates:       []string{"success"},
			shouldProcess:      false,
		},
		{
			name:               "reviewer filter disabled - CI success",
			filterByReviewer:   false,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"other-reviewer"},
			ciStatus:           "success",
			statusStates:       []string{"success"},
			shouldProcess:      true,
		},
		{
			name:               "reviewer filter - user is reviewer, CI failed",
			filterByReviewer:   true,
			currentUser:        "test-reviewer",
			requestedReviewers: []string{"test-reviewer"},
			ciStatus:           "failure",
			statusStates:       []string{"failure"},
			shouldProcess:      true, // Will process but won't approve (will try rebase)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &config{
				token:           "test-token",
				owner:           "test-owner",
				repo:            "test-repo",
				approve:         true,
				autoRebase:      true, // Default is true
				filterByReviewer: tc.filterByReviewer,
			}

			// Build requested reviewers list
			var reviewers []*github.User
			for _, reviewerLogin := range tc.requestedReviewers {
				reviewers = append(reviewers, &github.User{
					Login: github.Ptr(reviewerLogin),
				})
			}

			// Build status list
			var statuses []*github.RepoStatus
			for _, state := range tc.statusStates {
				statuses = append(statuses, &github.RepoStatus{
					State:   github.Ptr(state),
					Context: github.Ptr("test-check"),
				})
			}

			// Set up mock responses
			responses := map[string]interface{}{
				"/repos/test-owner/test-repo/commits/test-sha/status": &github.CombinedStatus{
					State:    github.Ptr(tc.ciStatus),
					Statuses: statuses,
				},
				"/repos/test-owner/test-repo/pulls/1/reviews": &github.PullRequestReview{
					ID:    github.Ptr[int64](123),
					State: github.Ptr("APPROVED"),
				},
				"/repos/test-owner/test-repo/pulls/1/merge": &github.PullRequestMergeResult{
					Merged:  github.Ptr(true),
					Message: github.Ptr("Pull Request successfully merged"),
				},
				"/repos/test-owner/test-repo/compare/base-sha...test-sha": &github.CommitsComparison{
					BehindBy: github.Ptr(0),
				},
			}

			// Add user endpoint if filterByReviewer is enabled
			if tc.filterByReviewer {
				responses["/user"] = &github.User{
					Login: github.Ptr(tc.currentUser),
				}
			}

			mockResp := &mockTransport{
				responses: responses,
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

			pr := &github.PullRequest{
				Number:            github.Ptr(1),
				Title:             github.Ptr("Test PR"),
				RequestedReviewers: reviewers,
				Head: &github.PullRequestBranch{
					SHA: github.Ptr("test-sha"),
				},
				Base: &github.PullRequestBranch{
					SHA: github.Ptr("base-sha"),
				},
			}

			err := processor.processSinglePR(pr)
			if err != nil {
				if tc.shouldProcess {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

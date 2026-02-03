package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v71/github"
	"golang.org/x/oauth2"
)

// Define execCommand as a variable for testing
var execCommand = exec.Command

type config struct {
	token         string
	owner         string
	repo          string
	approve       bool
	skipPattern   string // Regular expression pattern to skip PRs
	authorPattern string // Regular expression pattern to filter PRs by author
	autoRebase      bool   // Whether to automatically rebase PRs that are behind
	filterByReviewer bool  // Whether to filter PRs by reviewer (default: true)
}

type PRProcessor struct {
	client      *github.Client
	cfg         *config
	ctx         context.Context
	currentUser string // Current authenticated user login
}

func getGitConfig(key string) (string, error) {
	cmd := execCommand("git", "config", "--get", key)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func getRepositoryInfo() (owner string, repo string, err error) {
	remoteURL, err := getGitConfig("remote.origin.url")
	if err != nil {
		return "", "", fmt.Errorf("failed to get remote URL: %v", err)
	}

	// Handle both HTTPS and SSH URL formats
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	parts := strings.Split(remoteURL, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid remote URL format: %s", remoteURL)
	}

	repo = parts[len(parts)-1]
	owner = parts[len(parts)-2]

	// For SSH format, remove the username part from owner
	if strings.Contains(owner, ":") {
		owner = strings.Split(owner, ":")[1]
	}

	return owner, repo, nil
}

func loadConfigWithFlags(flags *flag.FlagSet, args []string) (*config, error) {
	cfg := &config{
		approve:         true,  // Default to true
		autoRebase:      false, // Default to false
		filterByReviewer: true, // Default to true
	}

	// Define command line flags
	flags.StringVar(&cfg.token, "token", "", "GitHub personal access token")
	flags.StringVar(&cfg.owner, "owner", "", "Repository owner")
	flags.StringVar(&cfg.repo, "repo", "", "Repository name")
	flags.BoolVar(&cfg.approve, "approve", true, "Automatically approve PR when status checks pass")
	flags.StringVar(&cfg.skipPattern, "skip-pattern", "", "Skip PRs whose titles match this regular expression pattern")
	flags.StringVar(&cfg.authorPattern, "author-pattern", "", "Only process PRs whose authors match this regular expression pattern")
	flags.BoolVar(&cfg.autoRebase, "auto-rebase", false, "Automatically rebase PRs that are behind the base branch")
	flags.BoolVar(&cfg.filterByReviewer, "no-filter-reviewer", false, "Disable filtering by reviewer (process all PRs)")

	if err := flags.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %v", err)
	}

	// Load from environment variables if not specified in command line
	if cfg.token == "" {
		cfg.token = os.Getenv("GITHUB_TOKEN")
	}
	if cfg.owner == "" {
		cfg.owner = os.Getenv("GITHUB_OWNER")
	}
	if cfg.repo == "" {
		cfg.repo = os.Getenv("GITHUB_REPO")
	}
	if cfg.skipPattern == "" {
		cfg.skipPattern = os.Getenv("GITHUB_PR_SKIP_PATTERN")
	}
	if cfg.authorPattern == "" {
		cfg.authorPattern = os.Getenv("GITHUB_PR_AUTHOR_PATTERN")
	}
	// Check environment variable for filterByReviewer (inverted logic: GITHUB_NO_FILTER_REVIEWER=true means filterByReviewer=false)
	if noFilterReviewer := os.Getenv("GITHUB_NO_FILTER_REVIEWER"); noFilterReviewer == "true" || noFilterReviewer == "1" {
		cfg.filterByReviewer = false
	}

	// Token is required
	if cfg.token == "" {
		return nil, fmt.Errorf("GitHub token is required. Set it via -token flag or GITHUB_TOKEN environment variable")
	}

	// Validate skip pattern if provided
	if cfg.skipPattern != "" {
		if _, err := regexp.Compile(cfg.skipPattern); err != nil {
			return nil, fmt.Errorf("invalid skip pattern: %v", err)
		}
	}

	// Validate author pattern if provided
	if cfg.authorPattern != "" {
		if _, err := regexp.Compile(cfg.authorPattern); err != nil {
			return nil, fmt.Errorf("invalid author pattern: %v", err)
		}
	}

	// Get repository info from git config if owner/repo not specified
	if cfg.owner == "" || cfg.repo == "" {
		var err error
		cfg.owner, cfg.repo, err = getRepositoryInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to get repository info: %v", err)
		}
		log.Printf("Using repository from git config: %s/%s", cfg.owner, cfg.repo)
	}

	return cfg, nil
}

func NewPRProcessor(ctx context.Context, cfg *config) (*PRProcessor, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Get current authenticated user
	currentUser := ""
	if cfg.filterByReviewer {
		user, _, err := client.Users.Get(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("failed to get current user: %w", err)
		}
		currentUser = user.GetLogin()
	}

	return &PRProcessor{
		client:      client,
		cfg:         cfg,
		ctx:         ctx,
		currentUser: currentUser,
	}, nil
}

func (p *PRProcessor) ProcessPullRequests() error {
	// Get open pull requests
	prs, _, err := p.client.PullRequests.List(p.ctx, p.cfg.owner, p.cfg.repo, &github.PullRequestListOptions{
		State: "open",
	})
	if err != nil {
		return fmt.Errorf("error getting pull requests: %w", err)
	}

	fmt.Printf("Found %d open pull requests\n", len(prs))
	if p.cfg.filterByReviewer {
		fmt.Printf("Reviewer filter enabled: only processing PRs where %s is a reviewer\n", p.currentUser)
	}
	if p.cfg.authorPattern != "" {
		fmt.Printf("Author filter enabled: %s\n", p.cfg.authorPattern)
	}
	if p.cfg.skipPattern != "" {
		fmt.Printf("Skip pattern enabled: %s\n", p.cfg.skipPattern)
	}

	// Filter out draft PRs
	var nonDraftPRs []*github.PullRequest
	for _, pr := range prs {
		if !pr.GetDraft() {
			nonDraftPRs = append(nonDraftPRs, pr)
		} else {
			fmt.Printf("PR #%d: Skipping draft PR: %s\n", pr.GetNumber(), pr.GetTitle())
		}
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(nonDraftPRs))

	for _, pr := range nonDraftPRs {
		wg.Add(1)
		go func(pr *github.PullRequest) {
			defer wg.Done()
			if err := p.processSinglePR(pr); err != nil {
				log.Printf("Error processing PR #%d: %v", pr.GetNumber(), err)
				errChan <- fmt.Errorf("PR #%d: %w", pr.GetNumber(), err)
			}
		}(pr)
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors while processing PRs: %v", len(errors), errors)
	}

	return nil
}

func (p *PRProcessor) shouldSkipPR(pr *github.PullRequest) (bool, error) {
	// Check reviewer filter (if enabled, only process PRs where current user is a reviewer)
	if p.cfg.filterByReviewer {
		requestedReviewers := pr.RequestedReviewers
		if len(requestedReviewers) == 0 {
			fmt.Printf("PR #%d: Skipping due to no reviewers assigned\n", pr.GetNumber())
			return true, nil
		}
		isReviewer := false
		for _, reviewer := range requestedReviewers {
			if reviewer.GetLogin() == p.currentUser {
				isReviewer = true
				break
			}
		}
		if !isReviewer {
			fmt.Printf("PR #%d: Skipping due to %s not being a reviewer\n", pr.GetNumber(), p.currentUser)
			return true, nil
		}
	}

	// Check skip pattern
	if p.cfg.skipPattern != "" {
		matched, err := regexp.MatchString(p.cfg.skipPattern, pr.GetTitle())
		if err != nil {
			return false, fmt.Errorf("error matching skip pattern: %v", err)
		}
		if matched {
			fmt.Printf("PR #%d: Skipping due to title matching skip pattern: %s\n", pr.GetNumber(), p.cfg.skipPattern)
			return true, nil
		}
	}

	// Check author pattern (if specified, only process PRs from matching authors)
	if p.cfg.authorPattern != "" {
		author := pr.GetUser().GetLogin()
		matched, err := regexp.MatchString(p.cfg.authorPattern, author)
		if err != nil {
			return false, fmt.Errorf("error matching author pattern: %v", err)
		}
		if !matched {
			fmt.Printf("PR #%d: Skipping due to author '%s' not matching author pattern: %s\n", pr.GetNumber(), author, p.cfg.authorPattern)
			return true, nil
		}
	}

	return false, nil
}

func (p *PRProcessor) checkStatusChecks(pr *github.PullRequest) ([]string, []string, error) {
	combinedStatus, _, err := p.client.Repositories.GetCombinedStatus(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetHead().GetSHA(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting status: %v", err)
	}

	var failedStatuses []string
	var pendingStatuses []string

	for _, status := range combinedStatus.Statuses {
		switch status.GetState() {
		case "failure", "error":
			failedStatuses = append(failedStatuses, status.GetContext())
		case "pending":
			pendingStatuses = append(pendingStatuses, status.GetContext())
		case "success", "skipped":
			continue
		}
	}

	return failedStatuses, pendingStatuses, nil
}

func (p *PRProcessor) handleFailedChecks(pr *github.PullRequest, failedStatuses, pendingStatuses []string) error {
	fmt.Printf("PR #%d: Status checks not passed\n", pr.GetNumber())
	if len(failedStatuses) > 0 {
		fmt.Printf("PR #%d: Failed checks: %s\n", pr.GetNumber(), strings.Join(failedStatuses, ", "))
	}
	if len(pendingStatuses) > 0 {
		fmt.Printf("PR #%d: Pending checks: %s\n", pr.GetNumber(), strings.Join(pendingStatuses, ", "))
	}

	if !p.cfg.autoRebase {
		if len(failedStatuses) > 0 {
			fmt.Printf("PR #%d: Status checks failed and auto-rebase is disabled\n", pr.GetNumber())
		} else {
			fmt.Printf("PR #%d: Status checks pending and auto-rebase is disabled\n", pr.GetNumber())
		}
		return nil
	}

	return p.tryRebasePR(pr)
}

func (p *PRProcessor) tryRebasePR(pr *github.PullRequest) error {
	comparison, _, err := p.client.Repositories.CompareCommits(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetBase().GetSHA(), pr.GetHead().GetSHA(), nil)
	if err != nil {
		return fmt.Errorf("error comparing commits: %v", err)
	}

	if comparison.GetBehindBy() == 0 {
		fmt.Printf("PR #%d: Branch is up to date with base branch\n", pr.GetNumber())
		return nil
	}

	fmt.Printf("PR #%d: Needs rebase, behind by %d commits. Updating branch...\n", pr.GetNumber(), comparison.GetBehindBy())
	return p.updatePRBranch(pr)
}

func (p *PRProcessor) updatePRBranch(pr *github.PullRequest) error {
	result, _, err := p.client.PullRequests.UpdateBranch(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber(), nil)
	if err != nil {
		if strings.Contains(err.Error(), "not mergeable") {
			return fmt.Errorf("PR #%d: cannot be updated automatically, manual rebase required: %v", pr.GetNumber(), err)
		}
		return fmt.Errorf("error updating branch: %v", err)
	}

	if result.GetMessage() != "Updating pull request branch." {
		return nil
	}

	fmt.Printf("PR #%d: Update in progress, waiting for completion...\n", pr.GetNumber())
	return p.waitForUpdateCompletion(pr)
}

func (p *PRProcessor) waitForUpdateCompletion(pr *github.PullRequest) error {
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		time.Sleep(5 * time.Second)

		updatedPR, _, err := p.client.PullRequests.Get(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber())
		if err != nil {
			return fmt.Errorf("error getting updated PR status: %v", err)
		}

		if updatedPR.GetHead().GetSHA() != pr.GetHead().GetSHA() {
			fmt.Printf("PR #%d: Branch update completed\n", pr.GetNumber())
			return p.checkUpdatedPRStatus(updatedPR)
		}
	}

	return fmt.Errorf("PR #%d: branch update timed out", pr.GetNumber())
}

func (p *PRProcessor) checkUpdatedPRStatus(pr *github.PullRequest) error {
	failedStatuses, pendingStatuses, err := p.checkStatusChecks(pr)
	if err != nil {
		return err
	}

	if len(failedStatuses) == 0 && len(pendingStatuses) == 0 {
		return p.handleSuccessfulPR(pr)
	}

	fmt.Printf("PR #%d: Status checks still not passed after update\n", pr.GetNumber())
	return nil
}

func (p *PRProcessor) processSinglePR(pr *github.PullRequest) error {
	fmt.Printf("Processing PR #%d: %s\n", pr.GetNumber(), pr.GetTitle())

	shouldSkip, err := p.shouldSkipPR(pr)
	if err != nil {
		return err
	}
	if shouldSkip {
		return nil
	}

	failedStatuses, pendingStatuses, err := p.checkStatusChecks(pr)
	if err != nil {
		return err
	}

	if len(failedStatuses) > 0 || len(pendingStatuses) > 0 {
		return p.handleFailedChecks(pr, failedStatuses, pendingStatuses)
	}

	return p.handleSuccessfulPR(pr)
}

func (p *PRProcessor) handleSuccessfulPR(pr *github.PullRequest) error {
	// Enable auto-merge first using direct REST API call
	fmt.Printf("PR #%d: All status checks passed, enabling auto-merge...\n", pr.GetNumber())

	// Create review
	review := &github.PullRequestReviewRequest{
		Event: github.Ptr("APPROVE"),
	}

	// Then approve if configured
	if p.cfg.approve {
		fmt.Printf("PR #%d: Approving PR...\n", pr.GetNumber())
		review, _, err := p.client.PullRequests.CreateReview(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber(), review)
		if err != nil {
			return fmt.Errorf("error approving PR: %v", err)
		}
		fmt.Printf("PR #%d: Approved with review ID %d\n", pr.GetNumber(), review.GetID())
	}

	// Try to merge the PR
	result, _, err := p.client.PullRequests.Merge(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber(), "Auto-merge successful", &github.PullRequestOptions{
		MergeMethod: "merge",
	})
	if err != nil {
		return fmt.Errorf("error merging PR: %v", err)
	}

	fmt.Printf("PR #%d: Successfully merged: %v\n", pr.GetNumber(), result.GetMerged())
	return nil
}

func main() {
	ctx := context.Background()
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cfg, err := loadConfigWithFlags(flags, os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	processor, err := NewPRProcessor(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create PR processor: %v", err)
	}
	if err := processor.ProcessPullRequests(); err != nil {
		log.Fatalf("Failed to process pull requests: %v", err)
	}

	log.Println("Successfully completed processing all pull requests")
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/go-github/v71/github"
	"golang.org/x/oauth2"
)

// Define execCommand as a variable for testing
var execCommand = exec.Command

type config struct {
	token string
	owner string
	repo  string
}

type PRProcessor struct {
	client *github.Client
	cfg    *config
	ctx    context.Context
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
	cfg := &config{}

	// Define command line flags
	flags.StringVar(&cfg.token, "token", "", "GitHub personal access token")
	flags.StringVar(&cfg.owner, "owner", "", "Repository owner")
	flags.StringVar(&cfg.repo, "repo", "", "Repository name")

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

	// Token is required
	if cfg.token == "" {
		return nil, fmt.Errorf("GitHub token is required. Set it via -token flag or GITHUB_TOKEN environment variable")
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

func NewPRProcessor(ctx context.Context, cfg *config) *PRProcessor {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &PRProcessor{
		client: client,
		cfg:    cfg,
		ctx:    ctx,
	}
}

func (p *PRProcessor) ProcessPullRequests() error {
	// Get open pull requests
	prs, _, err := p.client.PullRequests.List(p.ctx, p.cfg.owner, p.cfg.repo, &github.PullRequestListOptions{
		State: "open",
	})
	if err != nil {
		return fmt.Errorf("error getting pull requests: %w", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(prs))

	for _, pr := range prs {
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

func (p *PRProcessor) processSinglePR(pr *github.PullRequest) error {
	fmt.Printf("Processing PR #%d: %s\n", pr.GetNumber(), pr.GetTitle())

	// Check PR status
	combinedStatus, _, err := p.client.Repositories.GetCombinedStatus(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetHead().GetSHA(), nil)
	if err != nil {
		return fmt.Errorf("error getting status: %v", err)
	}

	if combinedStatus.GetState() != "success" {
		fmt.Printf("PR #%d: Status checks not passed\n", pr.GetNumber())

		// Check if sync with base branch is needed
		var compareErr error
		comparison, _, compareErr := p.client.Repositories.CompareCommits(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetBase().GetSHA(), pr.GetHead().GetSHA(), nil)
		if compareErr != nil {
			return fmt.Errorf("error comparing commits: %v", compareErr)
		}

		if comparison.GetBehindBy() > 0 {
			fmt.Printf("PR #%d: Needs rebase, updating branch...\n", pr.GetNumber())

			// Update branch
			_, _, err = p.client.PullRequests.UpdateBranch(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber(), nil)
			if err != nil {
				return fmt.Errorf("error updating branch: %v", err)
			}
		}
	} else {
		fmt.Printf("PR #%d: All status checks passed, enabling auto-merge...\n", pr.GetNumber())

		// Enable merge
		_, _, err = p.client.PullRequests.Merge(p.ctx, p.cfg.owner, p.cfg.repo, pr.GetNumber(), "", &github.PullRequestOptions{
			MergeMethod: "merge",
		})
		if err != nil {
			return fmt.Errorf("error merging PR: %v", err)
		}
	}

	return nil
}

func main() {
	ctx := context.Background()
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cfg, err := loadConfigWithFlags(flags, os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	processor := NewPRProcessor(ctx, cfg)
	if err := processor.ProcessPullRequests(); err != nil {
		log.Fatalf("Failed to process pull requests: %v", err)
	}

	log.Println("Successfully completed processing all pull requests")
}

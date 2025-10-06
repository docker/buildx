package ghutil

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/pkg/errors"
)

func GithubActionsContext() ([]byte, error) {
	m := make(map[string]any)

	// Detect if running in GitHub Actions
	// "GITHUB_ACTIONS": "true"
	if gha, ok := os.LookupEnv("GITHUB_ACTIONS"); !ok {
		return nil, nil
	} else if v, _ := strconv.ParseBool(gha); !v {
		return nil, nil
	}

	// Skip if required event information is not available
	githubEventName := os.Getenv("GITHUB_EVENT_NAME")
	githubEventPath := os.Getenv("GITHUB_EVENT_PATH")
	if githubEventName == "" || githubEventPath == "" {
		return nil, nil
	}

	// GitHub event
	// "GITHUB_EVENT_NAME": "push"
	// "GITHUB_EVENT_PATH": "/home/runner/work/_temp/_github_workflow/event.json"
	m["github_event_name"] = githubEventName
	dt, err := os.ReadFile(githubEventPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read GITHUB_EVENT_PATH %q", githubEventPath)
	}
	var evt map[string]any
	if err := json.Unmarshal(dt, &evt); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal GitHub event payload from %q", githubEventPath)
	}
	m["github_event_payload"] = evt

	// GitHub actor
	// "GITHUB_ACTOR": "crazy-max"
	// "GITHUB_ACTOR_ID": "1951866"
	// "GITHUB_TRIGGERING_ACTOR": "crazy-max"
	githubActor := os.Getenv("GITHUB_ACTOR")
	githubActorID := os.Getenv("GITHUB_ACTOR_ID")
	githubTriggeringActor := os.Getenv("GITHUB_TRIGGERING_ACTOR")
	if githubActor != "" && githubActorID != "" && githubTriggeringActor != "" {
		m["github_actor"] = githubActor
		m["github_actor_id"] = githubActorID
		m["github_triggering_actor"] = githubTriggeringActor
	}

	// GitHub ref
	// "GITHUB_REF": "refs/heads/master"
	// "GITHUB_REF_NAME": "master"
	// "GITHUB_REF_PROTECTED": "true"
	// "GITHUB_REF_TYPE": "branch"
	githubRef := os.Getenv("GITHUB_REF")
	githubRefName := os.Getenv("GITHUB_REF_NAME")
	githubRefProtected := os.Getenv("GITHUB_REF_PROTECTED")
	githubRefType := os.Getenv("GITHUB_REF_TYPE")
	if githubRef != "" && githubRefName != "" && githubRefProtected != "" && githubRefType != "" {
		m["github_ref"] = githubRef
		m["github_ref_name"] = githubRefName
		m["github_ref_protected"] = githubRefProtected
		m["github_ref_type"] = githubRefType
	}

	// GitHub repository
	// "GITHUB_REPOSITORY": "crazy-max/ghaction-dump-context"
	// "GITHUB_REPOSITORY_ID": "286865579"
	// "GITHUB_REPOSITORY_OWNER": "crazy-max"
	// "GITHUB_REPOSITORY_OWNER_ID": "1951866"
	githubRepository := os.Getenv("GITHUB_REPOSITORY")
	githubRepositoryID := os.Getenv("GITHUB_REPOSITORY_ID")
	githubRepositoryOwner := os.Getenv("GITHUB_REPOSITORY_OWNER")
	githubRepositoryOwnerID := os.Getenv("GITHUB_REPOSITORY_OWNER_ID")
	if githubRepository != "" && githubRepositoryID != "" && githubRepositoryOwner != "" && githubRepositoryOwnerID != "" {
		m["github_repository"] = githubRepository
		m["github_repository_id"] = githubRepositoryID
		m["github_repository_owner"] = githubRepositoryOwner
		m["github_repository_owner_id"] = githubRepositoryOwnerID
	}

	// GitHub workflow
	// "GITHUB_JOB": "dump"
	// "GITHUB_RUN_ATTEMPT": "1"
	// "GITHUB_RUN_ID": "18715517508"
	// "GITHUB_RUN_NUMBER": "617"
	// "GITHUB_SERVER_URL": "https://github.com"
	// "GITHUB_WORKFLOW": "ci"
	// "GITHUB_WORKFLOW_REF": "crazy-max/ghaction-dump-context/.github/workflows/ci.yml@refs/heads/master"
	// "GITHUB_WORKFLOW_SHA": "391d493f5fb04040f2a2770b33f86f3f62518392"
	githubJob := os.Getenv("GITHUB_JOB")
	githubRunAttempt := os.Getenv("GITHUB_RUN_ATTEMPT")
	githubRunID := os.Getenv("GITHUB_RUN_ID")
	githubRunNumber := os.Getenv("GITHUB_RUN_NUMBER")
	githubServerURL := os.Getenv("GITHUB_SERVER_URL")
	githubWorkflow := os.Getenv("GITHUB_WORKFLOW")
	githubWorkflowRef := os.Getenv("GITHUB_WORKFLOW_REF")
	githubWorkflowSHA := os.Getenv("GITHUB_WORKFLOW_SHA")
	if githubJob != "" && githubRunAttempt != "" && githubRunID != "" && githubRunNumber != "" && githubServerURL != "" && githubWorkflow != "" && githubWorkflowRef != "" && githubWorkflowSHA != "" {
		m["github_job"] = githubJob
		m["github_run_attempt"] = githubRunAttempt
		m["github_run_id"] = githubRunID
		m["github_run_number"] = githubRunNumber
		m["github_server_url"] = githubServerURL
		m["github_workflow"] = githubWorkflow
		m["github_workflow_ref"] = githubWorkflowRef
		m["github_workflow_sha"] = githubWorkflowSHA
	}

	// GitHub runner information
	// "ImageOS": "ubuntu24"
	// "ImageVersion": "20250929.60.1"
	// "RUNNER_ARCH": "X64"
	// "RUNNER_ENVIRONMENT": "github-hosted"
	// "RUNNER_NAME": "GitHub Actions 1000091440"
	// "RUNNER_OS": "Linux"
	// "RUNNER_TEMP": "/home/runner/work/_temp"
	// "RUNNER_TOOL_CACHE": "/opt/hostedtoolcache"
	// "RUNNER_TRACKING_ID": "github_7bd1d852-eaeb-423f-8fa4-4ddb797c652f"
	// "RUNNER_WORKSPACE": "/home/runner/work/ghaction-dump-context"
	githubRunnerImageOS := os.Getenv("ImageOS")
	githubRunnerImageVersion := os.Getenv("ImageVersion")
	githubRunnerOS := os.Getenv("RUNNER_OS")
	githubRunnerArch := os.Getenv("RUNNER_ARCH")
	githubRunnerEnvironment := os.Getenv("RUNNER_ENVIRONMENT")
	githubRunnerTrackingID := os.Getenv("RUNNER_TRACKING_ID")
	githubRunnerName := os.Getenv("RUNNER_NAME")
	if githubRunnerImageOS != "" && githubRunnerImageVersion != "" && githubRunnerOS != "" && githubRunnerArch != "" && githubRunnerEnvironment != "" && githubRunnerTrackingID != "" && githubRunnerName != "" {
		m["github_runner_image_os"] = githubRunnerImageOS
		m["github_runner_image_version"] = githubRunnerImageVersion
		m["github_runner_os"] = githubRunnerOS
		m["github_runner_arch"] = githubRunnerArch
		m["github_runner_environment"] = githubRunnerEnvironment
		m["github_runner_tracking_id"] = githubRunnerTrackingID
		m["github_runner_name"] = githubRunnerName
	}

	return json.Marshal(m)
}

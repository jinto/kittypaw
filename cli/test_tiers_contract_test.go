package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakefileDefinesTestAndEvalTiers(t *testing.T) {
	body := readRepoFile(t, "Makefile")
	for _, target := range []string{
		"test-unit",
		"test-integration",
		"test-e2e",
		"test-ci",
		"eval-secretary",
		"eval-user-flows",
		"eval-local",
		"smoke",
	} {
		if !strings.Contains(body, "\n"+target+":") {
			t.Fatalf("Makefile missing target %q", target)
		}
	}
	for _, want := range []string{
		"go test ./... -v -count=1 -short",
		"go test -tags integration ./... -v -count=1",
		"go test -tags e2e ./... -v -count=1",
		"eval/secretary_smoke/run.sh",
		"eval/user_vision_flows/run.sh",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Makefile missing command %q", want)
		}
	}
}

func TestEvalRunnersExposeStateExitContract(t *testing.T) {
	for _, path := range []string{
		"eval/secretary_smoke/run.sh",
		"eval/user_vision_flows/run.sh",
	} {
		body := readRepoFile(t, path)
		for _, token := range []string{"PASS", "FAIL", "INFRA", "NOT_RUN", "STATE:"} {
			if !strings.Contains(body, token) {
				t.Fatalf("%s missing eval state token %q", path, token)
			}
		}
		for _, contract := range []string{
			"finish PASS 0",
			"finish FAIL 1",
			"finish INFRA 2",
			"finish NOT_RUN 3",
		} {
			if !strings.Contains(body, contract) {
				t.Fatalf("%s missing exit contract %q", path, contract)
			}
		}
	}
}

func TestSecretarySmokeTreatsJudgeAndChatFailuresAsInfra(t *testing.T) {
	body := readRepoFile(t, "eval/secretary_smoke/run.sh")
	for _, want := range []string{
		"judge_behavior",
		"return 2",
		"finish INFRA 2",
		"chat command failed",
		"judge request failed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("secretary smoke runner missing infra guard %q", want)
		}
	}
	if strings.Contains(body, `chat "$input" 2>&1 || true`) {
		t.Fatal("secretary smoke runner must not mask kittypaw chat failures with || true")
	}
}

func TestUserVisionFlowsUseBehaviorJudgeAndProviderBaselines(t *testing.T) {
	body := readRepoFile(t, "eval/user_vision_flows/run.sh")
	for _, want := range []string{
		"BASELINE_FILE",
		"provider_family",
		"load_provider_baselines",
		"judge_flow_behavior",
		"FLOW_BEHAVIORS",
		"FLOW_THRESHOLDS",
		"record_flow",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("user vision flow runner missing behavior/baseline hook %q", want)
		}
	}
	for _, brittle := range []string{
		`assert_contains "live-rates" "Frankfurter"`,
		`assert_contains "install-ack" "✅"`,
		`assert_contains "clarify" "환율 말씀이세요"`,
		`assert_contains "chitchat-ack" "도움이 됐다니"`,
	} {
		if strings.Contains(body, brittle) {
			t.Fatalf("user vision flow runner still has brittle substring assertion %q", brittle)
		}
	}

	baseline := readRepoFile(t, "eval/user_vision_flows/provider_baselines.json")
	for _, provider := range []string{"anthropic", "openai", "gemini", "default"} {
		if !strings.Contains(baseline, `"`+provider+`"`) {
			t.Fatalf("provider baseline missing %q", provider)
		}
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

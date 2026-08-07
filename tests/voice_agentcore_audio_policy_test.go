package tests

import (
	"strings"
	"testing"
)

func TestAgentCoreProductionImageFailsClosedWithoutARM64Libopus(t *testing.T) {
	t.Parallel()

	dockerfile := readRepositoryFile(t, "deploy", "agentcore", "Dockerfile")
	for _, required := range []string{
		"FROM --platform=$TARGETPLATFORM golang:1.25-bookworm AS build",
		"libopus-dev",
		"pkg-config",
		"CGO_ENABLED=1",
		"-tags=voice_libopus",
		`go test -count=1 -tags="voice_libopus voice_libopus_test" ./internal/voice/audio`,
		"COPY internal ./internal",
		"COPY pkg ./pkg",
		"/usr/lib/aarch64-linux-gnu/libopus.so.0*",
		"FROM scratch AS voice-runtime-artifact",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("AgentCore Dockerfile is missing libopus build invariant %q", required)
		}
	}
	for _, forbidden := range []string{"CGO_ENABLED=0", "libopusfile"} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("AgentCore Dockerfile contains forbidden codec build surface %q", forbidden)
		}
	}

	dockerignore := readRepositoryFile(t, ".dockerignore")
	broadTestIgnore := strings.Index(dockerignore, "**/*_test.go")
	audioTestAllowlist := strings.Index(dockerignore, "!internal/voice/audio/*_test.go")
	if broadTestIgnore < 0 || audioTestAllowlist < broadTestIgnore {
		t.Fatal("Docker context must include the tagged audio tests executed by the ARM64 build")
	}
}

func TestAgentCoreEntrypointVerifiesCodecBeforeListening(t *testing.T) {
	t.Parallel()

	entrypoint := readRepositoryFile(t, "cmd", "agentcore", "voice-runtime", "main.go")
	importIndex := strings.Index(entrypoint, `"invoice-backend/internal/voice/audio"`)
	verifyIndex := strings.Index(entrypoint, "audio.VerifyLibopus()")
	listenIndex := strings.Index(entrypoint, "httpServer.ListenAndServe()")
	if importIndex < 0 || verifyIndex < 0 || listenIndex < 0 {
		t.Fatalf("AgentCore entrypoint must import, verify, and then serve with libopus")
	}
	if verifyIndex > listenIndex {
		t.Fatal("AgentCore entrypoint must verify libopus before opening its listener")
	}
}

func TestAgentCoreBuildAndCIExerciseOnlyTheCodecEnabledImage(t *testing.T) {
	t.Parallel()

	makefile := readRepositoryFile(t, "Makefile")
	buildRecipe := makeTargetRecipe(t, makefile, "build-agentcore")
	for _, required := range []string{
		"docker buildx build",
		"--platform linux/arm64",
		"--file deploy/agentcore/Dockerfile",
		"--target voice-runtime-artifact",
	} {
		if !strings.Contains(buildRecipe, required) {
			t.Errorf("build-agentcore must use the codec-enabled image build: missing %q", required)
		}
	}
	for _, forbidden := range []string{"CGO_ENABLED=0", "go build"} {
		if strings.Contains(buildRecipe, forbidden) {
			t.Errorf("build-agentcore may not bypass the codec-enabled Dockerfile with %q", forbidden)
		}
	}

	testRecipe := makeTargetRecipe(t, makefile, "test-agentcore")
	if !strings.Contains(testRecipe, "./internal/voice/audio") {
		t.Fatal("test-agentcore must run the pure audio contract under the race detector")
	}
	for _, required := range []string{
		"./internal/providers/sarvam",
		"./internal/voice/composition",
		"./internal/voice/session",
		"./internal/voice/tools",
		"./internal/voice/turn",
	} {
		if !strings.Contains(testRecipe, required) {
			t.Errorf("test-agentcore must exercise the production voice dependency graph: missing %q", required)
		}
	}

	workflow := readRepositoryFile(t, ".github", "workflows", "deploy.yml")
	verifyStart := strings.Index(workflow, "  verify:")
	deployStart := strings.Index(workflow, "  deploy:")
	if verifyStart < 0 || deployStart < verifyStart {
		t.Fatal("CI workflow must define verify before deploy")
	}
	verifyJob := workflow[verifyStart:deployStart]
	nativeARM64 := strings.Contains(verifyJob, "runs-on: ubuntu-24.04-arm")
	qemuIndex := strings.Index(verifyJob, "docker/setup-qemu-action@v3")
	buildxIndex := strings.Index(verifyJob, "docker/setup-buildx-action@v3")
	makeBuildIndex := strings.Index(verifyJob, "run: make build-agentcore")
	if buildxIndex < 0 || makeBuildIndex < 0 || buildxIndex > makeBuildIndex || (!nativeARM64 && (qemuIndex < 0 || qemuIndex > makeBuildIndex)) {
		t.Fatal("CI must use native ARM64 or configure ARM64 emulation, then configure Buildx before build-agentcore")
	}
	for _, required := range []string{
		"docker network create --internal billeif-voice-offline-ci",
		"docker run --detach",
		"--network billeif-voice-offline-ci",
		"billeif-voice-runtime:ci",
		"http://127.0.0.1:18080/ping",
		`{"status":"Healthy"}`,
		"AWS_EC2_METADATA_DISABLED=true",
		"AWS_ACCESS_KEY_ID=ci-offline",
		"AWS_SECRET_ACCESS_KEY=ci-offline-secret",
		"RUNTIME_ID=ci_voice_runtime",
		"BILLEIF_API_ORIGIN=https://api123.execute-api.ap-south-1.amazonaws.com/test",
		"SARVAM_SECRET_ARN=arn:aws:secretsmanager:ap-south-1:123456789012:secret:ci-sarvam",
		"VOICE_KVS_CHANNEL_COUNT=12",
		"VOICE_PROTOCOL_VERSION=1",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CI must prove the ARM64 runtime can load libopus: missing %q", required)
		}
	}
	if !strings.Contains(workflow, "docker network rm billeif-voice-offline-ci") {
		t.Fatal("CI bootstrap smoke must remove its internal-only Docker network")
	}
	for _, forbidden := range []string{
		"SARVAM_API_KEY=",
		"VOICE_RUNTIME_BOOTSTRAP_MODE=",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("CI bootstrap smoke must remain provider-free and use production wiring: found %q", forbidden)
		}
	}
	publishIndex := strings.Index(workflow, "--push .")
	deployQEMUIndex := strings.LastIndex(workflow, "docker/setup-qemu-action@v3")
	deployBuildxIndex := strings.LastIndex(workflow, "docker/setup-buildx-action@v3")
	if strings.Count(workflow, "docker/setup-qemu-action@v3") != 1 || publishIndex < 0 ||
		deployQEMUIndex > publishIndex || deployBuildxIndex > publishIndex || deployQEMUIndex > deployBuildxIndex {
		t.Fatal("the separately hosted deploy job must configure ARM64 emulation before publishing")
	}
}

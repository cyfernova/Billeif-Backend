package tests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOutboxLambdaBuildAndPackageAreSmallAndDeterministic(t *testing.T) {
	command := exec.Command("make", "-n", "package-lambda-outbox")
	command.Dir = ".."
	body, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render outbox package recipe: %v\n%s", err, body)
	}
	recipe := string(body)
	for _, required := range []string{
		"mkdir -p .build/lambda/outbox",
		"GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags=\"-s -w -buildid=\" -o $(LAMBDA_BUILD_DIR)/outbox/bootstrap ./cmd/lambda/outbox",
		"rm -f .build/lambda/outbox.zip",
		"TZ=UTC touch -t 198001010000 .build/lambda/outbox/bootstrap",
		"cd .build/lambda/outbox && TZ=UTC zip -q -X -j ../outbox.zip bootstrap",
	} {
		required = strings.ReplaceAll(required, "$(LAMBDA_BUILD_DIR)", ".build/lambda")
		if !strings.Contains(recipe, required) {
			t.Fatalf("outbox package recipe is missing behavior %q\n%s", required, recipe)
		}
	}
}

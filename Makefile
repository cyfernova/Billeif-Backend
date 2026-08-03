# Load .env file if it exists
-include .env
-include .env.local
export

.PHONY: help infra-backend-init infra-init infra-validate infra-apply infra-plan infra-destroy infra-output build-lambda build-lambda-http build-lambda-a2a-stream build-lambda-sqs-invoice build-lambda-sqs-email-delivery build-lambda-sqs-ses-feedback build-lambda-sqs-gst build-lambda-sqs-bargaining build-lambda-ws build-lambda-voice-session build-lambda-outbox build-lambda-migrator build-lambda-custom-sms-sender package-lambda package-lambda-email-delivery package-lambda-ses-feedback package-lambda-outbox package-lambda-migrator migration-manifest migration-manifest-verify rds-tunnel run-local test test-integration migrate-up migrate-down migrate-rds-up migrate-rds-down migrate-create fmt lint clean deps test-coverage swagger

LAMBDA_BUILD_DIR := .build/lambda
TERRAFORM_DIR := infrastructure/terraform
AWS_PROFILE ?= default
TF_BACKEND_BUCKET ?= billeif-terraform-state-928282274753-ap-south-1
TF_BACKEND_REGION ?= ap-south-1
TF_BACKEND_LOCK_TABLE ?= billeif-terraform-state-lock
TF_BACKEND_KEY ?= billeif/dev/terraform.tfstate
RDS_LOCAL_PORT ?= 15432
TF_VAR_india_sms_sender_id ?= $(INDIA_SMS_SENDER_ID)
TF_VAR_india_dlt_entity_id ?= $(INDIA_DLT_ENTITY_ID)
TF_VAR_india_signup_template_id ?= $(INDIA_SIGNUP_TEMPLATE_ID)
TF_VAR_india_auth_template_id ?= $(INDIA_AUTH_TEMPLATE_ID)
TF_VAR_llm_api_url ?= $(LLM_API_URL)
TF_VAR_llm_model ?= $(LLM_MODEL)
TF_VAR_exa_base_url ?= $(EXA_BASE_URL)
TF_VAR_exa_timeout ?= $(EXA_TIMEOUT)
TF_VAR_gst_lookup_base_url ?= $(GST_LOOKUP_BASE_URL)
TF_VAR_gst_lookup_timeout ?= $(GST_LOOKUP_TIMEOUT)
TF_VAR_deepgram_voice_agent_url ?= $(DEEPGRAM_VOICE_AGENT_URL)
TF_VAR_deepgram_voice_listen_model ?= $(DEEPGRAM_VOICE_LISTEN_MODEL)
TF_VAR_deepgram_voice_speak_model ?= $(DEEPGRAM_VOICE_SPEAK_MODEL)
TF_VAR_deepgram_voice_input_encoding ?= $(DEEPGRAM_VOICE_INPUT_ENCODING)
TF_VAR_deepgram_voice_input_sample_rate ?= $(DEEPGRAM_VOICE_INPUT_SAMPLE_RATE)
TF_VAR_deepgram_voice_output_encoding ?= $(DEEPGRAM_VOICE_OUTPUT_ENCODING)
TF_VAR_deepgram_voice_output_sample_rate ?= $(DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE)
TF_VAR_deepseek_base_url ?= $(DEEPSEEK_BASE_URL)
TF_VAR_deepseek_model ?= $(DEEPSEEK_MODEL)
TF_VAR_mcp_server_url ?= $(MCP_SERVER_URL)
TF_VAR_voice_ws_max_session_seconds ?= $(VOICE_WS_MAX_SESSION_SECONDS)
TF_VAR_voice_ws_ping_interval_seconds ?= $(VOICE_WS_PING_INTERVAL_SECONDS)
TF_VAR_voice_ws_write_timeout_seconds ?= $(VOICE_WS_WRITE_TIMEOUT_SECONDS)
TF_VAR_voice_ws_max_frame_bytes ?= $(VOICE_WS_MAX_FRAME_BYTES)
TF_VAR_voice_ws_max_concurrent_sessions_per_user ?= $(VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER)
ifneq ($(strip $(ENABLE_COGNITO_CUSTOM_DOMAIN_CUTOVER)),)
TF_VAR_enable_cognito_custom_domain_cutover ?= $(ENABLE_COGNITO_CUSTOM_DOMAIN_CUTOVER)
endif
ifneq ($(strip $(ENABLE_COGNITO_CUSTOM_DOMAIN_PROVISIONING)),)
TF_VAR_enable_cognito_custom_domain_provisioning ?= $(ENABLE_COGNITO_CUSTOM_DOMAIN_PROVISIONING)
endif
TF_INIT_BACKEND_ARGS := \
	-backend-config=bucket=$(TF_BACKEND_BUCKET) \
	-backend-config=region=$(TF_BACKEND_REGION) \
	-backend-config=key=$(TF_BACKEND_KEY) \
	-backend-config=encrypt=true \
	-backend-config=use_lockfile=true

help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Infrastructure targets
infra-backend-init: ## Create S3 bucket and DynamoDB table for Terraform backend
	@echo "Creating S3 bucket for Terraform state..."
	aws s3 mb s3://$(TF_BACKEND_BUCKET) --profile $(AWS_PROFILE) --region $(TF_BACKEND_REGION) || true
	aws s3api put-bucket-versioning --bucket $(TF_BACKEND_BUCKET) --versioning-configuration Status=Enabled --profile $(AWS_PROFILE) --region $(TF_BACKEND_REGION)
	@echo "Creating DynamoDB table for state locking..."
	aws dynamodb create-table \
		--table-name $(TF_BACKEND_LOCK_TABLE) \
		--attribute-definitions AttributeName=LockID,AttributeType=S \
		--key-schema AttributeName=LockID,KeyType=HASH \
		--billing-mode PAY_PER_REQUEST \
		--profile $(AWS_PROFILE) \
		--region $(TF_BACKEND_REGION) || true
	@echo "Backend resources created!"

infra-init: ## Initialize Terraform
	cd $(TERRAFORM_DIR) && terraform init $(TF_INIT_BACKEND_ARGS)

infra-validate: ## Format-check and validate Terraform without the remote backend
	cd $(TERRAFORM_DIR) && terraform fmt -check -recursive
	cd $(TERRAFORM_DIR) && terraform init -backend=false
	cd $(TERRAFORM_DIR) && terraform validate

infra-apply: package-lambda ## Package Lambda artifacts and apply Terraform configuration
	cd $(TERRAFORM_DIR) && terraform init $(TF_INIT_BACKEND_ARGS)
	cd $(TERRAFORM_DIR) && terraform apply -auto-approve

infra-plan: package-lambda ## Package Lambda artifacts and plan Terraform configuration
	cd $(TERRAFORM_DIR) && terraform init $(TF_INIT_BACKEND_ARGS)
	cd $(TERRAFORM_DIR) && terraform plan

infra-destroy: ## Destroy Terraform infrastructure
	cd $(TERRAFORM_DIR) && terraform init $(TF_INIT_BACKEND_ARGS)
	cd $(TERRAFORM_DIR) && terraform destroy -auto-approve

infra-output: ## Save Terraform output to file
	cd $(TERRAFORM_DIR) && terraform output -json > terraform_output.json
	cd $(TERRAFORM_DIR) && terraform output > terraform_output.txt
	@echo "Terraform output saved to infrastructure/terraform/terraform_output.txt"

# Build targets
build-lambda: build-lambda-http build-lambda-a2a-stream build-lambda-sqs-invoice build-lambda-sqs-email-delivery build-lambda-sqs-ses-feedback build-lambda-sqs-gst build-lambda-sqs-bargaining build-lambda-ws build-lambda-voice-session build-lambda-outbox build-lambda-migrator ## Build all Lambda binaries

build-lambda-http: ## Build HTTP API Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/http
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/http/bootstrap ./cmd/lambda/http

build-lambda-a2a-stream: ## Build A2A stream Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/a2a-stream
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/a2a-stream/bootstrap ./cmd/lambda/a2a-stream

build-lambda-sqs-invoice: ## Build invoice SQS Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-invoice
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/sqs-invoice/bootstrap ./cmd/lambda/sqs-invoice

build-lambda-sqs-email-delivery: ## Build stripped ARM64 Billeif email delivery Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-email-delivery
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/sqs-email-delivery/bootstrap ./cmd/lambda/sqs-email-delivery

build-lambda-sqs-ses-feedback: ## Build stripped ARM64 Billeif SES feedback Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-ses-feedback
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/sqs-ses-feedback/bootstrap ./cmd/lambda/sqs-ses-feedback

build-lambda-sqs-gst: ## Build GST SQS Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-gst
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/sqs-gst/bootstrap ./cmd/lambda/sqs-gst

build-lambda-sqs-bargaining: ## Build bargaining SQS Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-bargaining
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/sqs-bargaining/bootstrap ./cmd/lambda/sqs-bargaining

build-lambda-ws: ## Build WebSocket Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/ws
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/ws/bootstrap ./cmd/lambda/ws

build-lambda-voice-session: ## Build realtime voice session Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/voice-session
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/voice-session/bootstrap ./cmd/lambda/voice-session

build-lambda-outbox: ## Build stripped ARM64 Billeif outbox dispatcher Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/outbox
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/outbox/bootstrap ./cmd/lambda/outbox

build-lambda-migrator: migration-manifest-verify ## Build stripped ARM64 database migration Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/migrator
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o $(LAMBDA_BUILD_DIR)/migrator/bootstrap ./cmd/lambda/migrator

build-lambda-custom-sms-sender: ## Build the Node.js custom SMS sender Lambda package
	rm -rf $(LAMBDA_BUILD_DIR)/custom-sms-sender
	mkdir -p $(LAMBDA_BUILD_DIR)/custom-sms-sender
	cp -R infrastructure/lambda/custom-sms-sender/. $(LAMBDA_BUILD_DIR)/custom-sms-sender/
	cd $(LAMBDA_BUILD_DIR)/custom-sms-sender && pnpm install --prod --frozen-lockfile

package-lambda: build-lambda build-lambda-custom-sms-sender package-lambda-email-delivery package-lambda-ses-feedback package-lambda-outbox package-lambda-migrator ## Package Lambda artifacts into zip files
	rm -f $(LAMBDA_BUILD_DIR)/http.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/http/bootstrap
	cd $(LAMBDA_BUILD_DIR)/http && TZ=UTC zip -q -X -j ../http.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/a2a-stream.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/a2a-stream/bootstrap
	cd $(LAMBDA_BUILD_DIR)/a2a-stream && TZ=UTC zip -q -X -j ../a2a-stream.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/sqs-invoice.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/sqs-invoice/bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-invoice && TZ=UTC zip -q -X -j ../sqs-invoice.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/sqs-gst.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/sqs-gst/bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-gst && TZ=UTC zip -q -X -j ../sqs-gst.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/sqs-bargaining.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/sqs-bargaining/bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-bargaining && TZ=UTC zip -q -X -j ../sqs-bargaining.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/ws.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/ws/bootstrap
	cd $(LAMBDA_BUILD_DIR)/ws && TZ=UTC zip -q -X -j ../ws.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/voice-session.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/voice-session/bootstrap
	cd $(LAMBDA_BUILD_DIR)/voice-session && TZ=UTC zip -q -X -j ../voice-session.zip bootstrap
	rm -f $(LAMBDA_BUILD_DIR)/custom-sms-sender.zip
	rm -f $(LAMBDA_BUILD_DIR)/custom-sms-sender/node_modules/.modules.yaml $(LAMBDA_BUILD_DIR)/custom-sms-sender/node_modules/.pnpm-workspace-state-v1.json
	find $(LAMBDA_BUILD_DIR)/custom-sms-sender -exec touch -t 198001010000 {} +
	cd $(LAMBDA_BUILD_DIR)/custom-sms-sender && find -L . -type f -print | LC_ALL=C sort | TZ=UTC zip -q -X ../custom-sms-sender.zip -@

package-lambda-outbox: build-lambda-outbox ## Package the Billeif outbox Lambda deterministically
	rm -f $(LAMBDA_BUILD_DIR)/outbox.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/outbox/bootstrap
	cd $(LAMBDA_BUILD_DIR)/outbox && TZ=UTC zip -q -X -j ../outbox.zip bootstrap

package-lambda-email-delivery: build-lambda-sqs-email-delivery ## Package the Billeif email delivery Lambda deterministically
	rm -f $(LAMBDA_BUILD_DIR)/sqs-email-delivery.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/sqs-email-delivery/bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-email-delivery && TZ=UTC zip -q -X -j ../sqs-email-delivery.zip bootstrap

package-lambda-ses-feedback: build-lambda-sqs-ses-feedback ## Package the Billeif SES feedback Lambda deterministically
	rm -f $(LAMBDA_BUILD_DIR)/sqs-ses-feedback.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/sqs-ses-feedback/bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-ses-feedback && TZ=UTC zip -q -X -j ../sqs-ses-feedback.zip bootstrap

package-lambda-migrator: build-lambda-migrator ## Package the migration Lambda deterministically
	rm -f $(LAMBDA_BUILD_DIR)/migrator.zip
	TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/migrator/bootstrap
	cd $(LAMBDA_BUILD_DIR)/migrator && TZ=UTC zip -q -X -j ../migrator.zip bootstrap

migration-manifest: ## Regenerate the deterministic root migration checksum manifest
	@set -euo pipefail; \
	TEMP_MANIFEST=$$(mktemp); \
	trap 'rm -f "$$TEMP_MANIFEST"' EXIT; \
	cd migrations; \
	find . -maxdepth 1 -type f \( -name '*.up.sql' -o -name '*.down.sql' \) -exec basename {} \; | LC_ALL=C sort | \
		while IFS= read -r MIGRATION_FILE; do shasum -a 256 "$$MIGRATION_FILE"; done > "$$TEMP_MANIFEST"; \
	mv "$$TEMP_MANIFEST" manifest.sha256; \
	trap - EXIT

migration-manifest-verify: ## Verify root migration files against the embedded checksum manifest
	cd migrations && shasum -a 256 -c manifest.sha256
	go test ./migrations -run TestEmbeddedBundleContainsEveryRootNumberedMigration -count=1

rds-tunnel: ## Forward localhost:RDS_LOCAL_PORT to private RDS through SSM
	@set -euo pipefail; \
	command -v aws >/dev/null || (echo "aws CLI is required" >&2; exit 1); \
	command -v session-manager-plugin >/dev/null || (echo "session-manager-plugin is required" >&2; exit 1); \
	cd $(TERRAFORM_DIR); \
	RDS_HOST=$$(terraform output -raw rds_address); \
	RDS_PORT=$$(terraform output -raw rds_port); \
	TARGET_ID="$(RDS_TUNNEL_TARGET_ID)"; \
	if [ -z "$$TARGET_ID" ]; then \
		TARGET_ID=$$(terraform output -raw rds_tunnel_instance_id); \
	fi; \
	if [ -z "$$TARGET_ID" ]; then \
		echo "RDS tunnel target not found. Apply Terraform or set RDS_TUNNEL_TARGET_ID." >&2; \
		exit 1; \
	fi; \
	echo "Forwarding 127.0.0.1:$(RDS_LOCAL_PORT) -> $$RDS_HOST:$$RDS_PORT via $$TARGET_ID"; \
	aws ssm start-session \
		--profile $(AWS_PROFILE) \
		--region $(TF_BACKEND_REGION) \
		--target "$$TARGET_ID" \
		--document-name AWS-StartPortForwardingSessionToRemoteHost \
		--parameters "{\"host\":[\"$$RDS_HOST\"],\"portNumber\":[\"$$RDS_PORT\"],\"localPortNumber\":[\"$(RDS_LOCAL_PORT)\"]}"

run-local: ## Run the HTTP server locally
	@set -euo pipefail; \
	if [ -n "$(strip $(DATABASE_HOST_SSM_PARAM))" ] && [ -z "$(strip $(DATABASE_HOST))" ]; then \
		echo "DATABASE_HOST is unset; using SSM tunnel endpoint 127.0.0.1:$(RDS_LOCAL_PORT). Start it in another terminal with: make rds-tunnel"; \
		DATABASE_HOST=127.0.0.1 DATABASE_PORT=$(RDS_LOCAL_PORT) go run ./cmd/server; \
	else \
		go run ./cmd/server; \
	fi

# Test targets
test: ## Run unit tests
	go test -v -race -cover ./...

test-integration: ## Run integration tests (requires local dependencies running)
	go test -v -tags=integration ./tests/integration/...

# Migration targets (using golang-migrate CLI)
migrate-up: ## Run database migrations up (using migrate CLI)
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is required for migrate-up" >&2; exit 1)
	migrate -path ./migrations -database "$$DATABASE_URL" up

migrate-down: ## Run database migrations down (using migrate CLI)
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is required for migrate-down" >&2; exit 1)
	migrate -path ./migrations -database "$$DATABASE_URL" down

migrate-rds-up: ## Run migrations against Terraform-managed RDS through asm-exec
	@set -euo pipefail; \
	command -v asm-exec >/dev/null || (echo "asm-exec is required for secret-safe RDS migrations" >&2; exit 1); \
	cd $(TERRAFORM_DIR); \
	DB_HOST=$$(terraform output -raw rds_address); \
	DB_PORT=$$(terraform output -raw rds_port); \
	DB_NAME=$$(terraform output -raw rds_database_name); \
	DB_SECRET_ARN=$$(terraform output -raw rds_master_user_secret_arn); \
	DB_URL="postgres://$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=require&connect_timeout=10"; \
	cd - >/dev/null; \
	AWS_PROFILE=default AWS_REGION=ap-south-1 asm-exec -- env \
		"PGUSER={{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:username}}" \
		"PGPASSWORD={{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:password}}" \
		migrate -path ./migrations -database "$$DB_URL" up

migrate-rds-down: ## Roll back one Terraform-managed RDS migration through asm-exec
	@set -euo pipefail; \
	command -v asm-exec >/dev/null || (echo "asm-exec is required for secret-safe RDS migrations" >&2; exit 1); \
	cd $(TERRAFORM_DIR); \
	DB_HOST=$$(terraform output -raw rds_address); \
	DB_PORT=$$(terraform output -raw rds_port); \
	DB_NAME=$$(terraform output -raw rds_database_name); \
	DB_SECRET_ARN=$$(terraform output -raw rds_master_user_secret_arn); \
	DB_URL="postgres://$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=require&connect_timeout=10"; \
	cd - >/dev/null; \
	AWS_PROFILE=default AWS_REGION=ap-south-1 asm-exec -- env \
		"PGUSER={{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:username}}" \
		"PGPASSWORD={{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:password}}" \
		migrate -path ./migrations -database "$$DB_URL" down 1

migrate-create: ## Create a new migration (usage: make migrate-create NAME=migration_name)
	migrate create -ext sql -dir ./migrations -seq $(NAME)

# Code quality targets
fmt: ## Format Go code
	go fmt ./...
	go run golang.org/x/tools/cmd/goimports@latest -w .

lint: ## Run linter
	golangci-lint run --timeout=5m

# Utility targets
clean: ## Clean build artifacts
	rm -rf .build/
	go clean

deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

test-coverage: ## Generate test coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

swagger: ## Generate Swagger documentation
	swag init -g internal/app/runtime.go -o docs/

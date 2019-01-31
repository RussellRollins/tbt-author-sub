BIN             = tbt-author-sub
LOCAL_BIN       = tbt-author-sub-local
LOCAL_DB_FILE   = .tbt
OUTPUT_DIR      = build
TF_DIR          = terraform

list:
	@grep '^[^#[:space:]].*:' Makefile

build/linux: clean ## Build a linux binary ready to be zip'ed for AWS Lambda Deployment
	mkdir -p $(OUTPUT_DIR) && cd cmd && GO111MODULE=on GOOS=linux CGO_ENABLED=0 go build -a -installsuffix cgo -o ../$(OUTPUT_DIR)/$(BIN) .

build/local: clean
	mkdir -p $(OUTPUT_DIR) && cd cmd && GO111MODULE=on CGO_ENABLED=0 go build -a -installsuffix cgo -o ../$(OUTPUT_DIR)/$(LOCAL_BIN) .

build: build/linux ## Zip linux binary as AWS Deployment archive
	cd $(OUTPUT_DIR) && zip $(BIN).zip $(BIN)

local: build/local ## Create MacOS binary, and run it w/ local envvar
	cd $(OUTPUT_DIR) && TBT_LOCAL=1 ./$(LOCAL_BIN)

local/reset:
	rm $(OUTPUT_DIR)/$(LOCAL_DB_FILE)

deploy: ## Deploy zip'ed archive to AWS production account
	cd $(TF_DIR) && terraform init && terraform apply

clean: clean/linux clean/local ## Remove all build artifacts

clean/linux: ## Remove linux build artifacts
	$(RM) $(OUTPUT_DIR)/$(BIN).zip
	$(RM) $(OUTPUT_DIR)/$(BIN)

clean/local:
	$(RM) $(OUTPUT_DIR)/$(LOCAL_BIN)

current-ref := $(shell git rev-parse HEAD)
.PHONY:build release upload

build:
	mkdir -p build
release:build/eb-package-$(current-ref).zip
build/eb-package-$(current-ref).zip:build Dockerfile .dockerignore $(shell find . -name "*.go")
	git archive --format zip --output build/eb-package-$(current-ref).zip HEAD ./
upload:build/eb-package-$(current-ref).zip
	aws s3 cp build/eb-package-$(current-ref).zip s3://build-push-testing/logkeeper/
	aws elasticbeanstalk create-application-version --application-name logkeeper --version-label testing-$(current-ref) \
							--description "testing with /health-check logging" \
							--source-bundle S3Bucket=build-push-testing,S3Key=logkeeper/eb-package-$(current-ref).zip
deploy-prod:upload
	aws elasticbeanstalk update-environment --application-name logkeeper --environment-id logkeeper-prod --version-label $(current-ref)
deploy-staging:upload
	aws elasticbeanstalk update-environment --application-name logkeeper --environment-id logkeeper-stage --version-label $(current-ref)
deploy-testing:upload
	aws elasticbeanstalk update-application --application-name logkeeper --environment-id testing --version-label $(current-ref)

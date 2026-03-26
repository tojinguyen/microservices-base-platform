.PHONY: build-identity run-identity clean

build-identity:
	@echo "Building identity service docker image..."
	docker build -t identity-service -f services/identity/Dockerfile .

run-identity: build-identity
	@echo "Running identity service docker container..."
	docker run -p 8080:8080 identity-service

clean:
	@echo "Cleaning up..."
	rm -rf bin/

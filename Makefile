.PHONY: client-dev cloud-dev clean build help

# Default target
help:
	@echo "Alfred Development Targets:"
	@echo "  client-dev  - Build and run the macOS menubar app in development"
	@echo "  cloud-dev   - Build and run the Go cloud server in development"
	@echo "  clean       - Clean build artifacts"
	@echo "  build       - Build both client and cloud"

# Client development (macOS menubar app)
client-dev:
	@echo "🚀 Starting Alfred menubar app in development mode..."
	@if [ ! -d "client/Alfred.xcodeproj" ]; then \
		echo "❌ Xcode project not found. Run 'make client-setup' first."; \
		exit 1; \
	fi
	@cd client && xcodebuild -project Alfred.xcodeproj -scheme Alfred -configuration Debug run

# Cloud development (Go server)
cloud-dev:
	@echo "🚀 Starting Alfred cloud server in development mode..."
	@if [ ! -f "cloud/go.mod" ]; then \
		echo "❌ Go module not found. Run 'make cloud-setup' first."; \
		exit 1; \
	fi
	@cd cloud && go mod tidy && go run api/main.go

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf client/build/
	@rm -rf client/DerivedData/
	@rm -f cloud/cloud
	@echo "✅ Clean complete"

# Build both targets
build: client cloud

# Setup client (one-time)
client-setup:
	@echo "⚙️ Setting up Alfred client..."
	@echo "📱 Please open client/Alfred.xcodeproj in Xcode to complete setup"

# Setup cloud (one-time)
cloud-setup:
	@echo "⚙️ Setting up Alfred cloud server..."
	@cd cloud && go mod tidy
	@echo "✅ Cloud setup complete"

# Build client only
client:
	@echo "🏗️ Building Alfred client..."
	@cd client && xcodebuild -project Alfred.xcodeproj -scheme Alfred -configuration Debug build
	@echo "✅ Client build complete"

# Build cloud only
cloud:
	@echo "🏗️ Building Alfred cloud server..."
	@cd cloud && go build -o cloud api/main.go
	@echo "✅ Cloud build complete"

# Test targets
test-client:
	@echo "🧪 Running client tests..."
	@cd client && xcodebuild test -project Alfred.xcodeproj -scheme Alfred -destination 'platform=macOS'

test-cloud:
	@echo "🧪 Running cloud server tests..."
	@cd cloud && go test ./...

test: test-client test-cloud

# Development environment check
dev-check:
	@echo "🔍 Checking development environment..."
	@which xcodebuild > /dev/null || (echo "❌ Xcode not found. Please install Xcode." && exit 1)
	@which go > /dev/null || (echo "❌ Go not found. Please install Go." && exit 1)
	@echo "✅ Development environment ready"
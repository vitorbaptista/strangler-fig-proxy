# [Optional] Customizations for the Go devcontainer
FROM mcr.microsoft.com/devcontainers/go:1.22

# Install Node.js and npm
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
RUN apt-get install -y nodejs

# Install @anthropic-ai/claude-code globally
RUN npm install -g @anthropic-ai/claude-code

# Install any additional OS packages or Go tools here
# RUN apt-get update && apt-get install -y <your-package>
# RUN go install golang.org/x/tools/cmd/goimports@latest 
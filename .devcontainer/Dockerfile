# [Optional] Customizations for the Go devcontainer
FROM mcr.microsoft.com/devcontainers/go:1.23

# Install Node.js and npm
RUN apt-get update && apt-get install -y curl python3-pip
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
RUN apt-get install -y nodejs

# Install @anthropic-ai/claude-code globally
RUN npm install -g @anthropic-ai/claude-code

# Install pre-commit globally
RUN pip install pre-commit --break-system-packages


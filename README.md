# synr
> A script that leaving from your unnecessary chat rooms

## Requirements
```bash
% go 1.9.2
% glide 0.13.0
```

## Installation
```bash
% glide up
```

## Setup
```bash
# Set tokens in config/secrets.yaml
- slack: https://api.slack.com/docs/oauth-test-tokens
- chatwork: http://developer.chatwork.com/ja/authenticate.html

# Build one binary
% go build
```

## Usage
```bash
# Show usage with options
% ./synr help

# Dry-run
% ./synr -c slack -d

# Run
% ./synr -c slack

# Run with selected months elapsed from last update (default: 1)
% ./synr -c slack -m 3

# Supported chats are slack and chatwork
% ./synr -c chatwork -m 3
```

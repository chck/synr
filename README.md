# break-upper
> A matchmaker to clean chats' unnecessary channels

## Requirements
```
go 1.7.0
glide 0.11.1
```

## Installation
```
glide up
```

## Setup
```
# Build one binary
go build

# Set tokens in config/secrets.yaml
- slack: https://api.slack.com/docs/oauth-test-tokens
- chatwork: http://developer.chatwork.com/ja/authenticate.html
```

## Usage
```
# Show usage with options
./break-upper help

# Dry-run
./break-upper -c slack -d

# Run
./break-upper -c slack

# Run with selected months elapsed from last update (default: 1)
./break-upper -c slack -m 3
```

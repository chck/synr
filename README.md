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

## Usage
```
# Build one binary
go build

# Show usage with options
./break-upper help

# Dry-run
./break-upper -c slack -d

# Run with selected months elapsed from last update (default: 1)
./break-upper -c slack -m 3
```

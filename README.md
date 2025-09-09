# Watchtower Proxy Configuration

## Environment Variables

- `WEBHOOK_ID` - Your unique webhook identifier (required)
- `WATCHTOWER_API_KEY` - API key for Watchtower authentication (required)
- `WATCHTOWER_URL` - Watchtower server URL (default: localhost:8080)
- `PORT` - Port for the proxy server (default: 3000)


## Example Configurations

### Standard Watchtower HTTP API
```bash
export WATCHTOWER_URL="localhost:8080"
export WATCHTOWER_ENDPOINT="/v1/update"
```

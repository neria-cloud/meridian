# Bifrost Gateway

Bifrost Gateway is a blazing-fast HTTP API that unifies access to 15+ AI providers (OpenAI, Anthropic, AWS Bedrock, Google Vertex, and more) through a single OpenAI-compatible interface. Deploy in seconds with zero configuration and get automatic fallbacks, semantic caching, tool calling, and enterprise-grade features.

**Complete Documentation**: [https://meridian.neria.cloud](https://meridian.neria.cloud)

---

## Quick Start

### Installation

Choose your preferred method:

#### NPX (Recommended)

```bash
# Install and run locally
npx -y @maximhq/bifrost

# Open web interface at http://localhost:8080
```

#### Docker

```bash
# Pull and run Bifrost Gateway
docker pull maximhq/bifrost
docker run -p 8080:8080 maximhq/bifrost

# For persistent configuration
docker run -p 8080:8080 -v $(pwd)/data:/app/data maximhq/bifrost
```

### Configuration

Bifrost starts with zero configuration needed. Configure providers through the **built-in web UI** at `http://localhost:8080` or via API:

```bash
# Add OpenAI provider via API
curl -X POST http://localhost:8080/api/providers \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "openai",
    "keys": [{"value": "sk-your-openai-key", "models": ["gpt-4o-mini"], "weight": 1.0}]
  }'
```

For file-based configuration, create `config.json` in your app directory:

```json
{
  "providers": {
    "openai": {
      "keys": [{"value": "env.OPENAI_API_KEY", "models": ["gpt-4o-mini"], "weight": 1.0}]
    }
  }
}
```

### Your First API Call

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello, Bifrost!"}]
  }'
```

**That's it!** You now have a unified AI gateway running locally.

---

## Key Features

Bifrost Gateway provides enterprise-grade AI infrastructure with these core capabilities:

### Core Features

- **[Unified Interface](https://meridian.neria.cloud/features/unified-interface)** - Single OpenAI-compatible API for all providers
- **[Multi-Provider Support](https://meridian.neria.cloud/quickstart/gateway/provider-configuration)** - OpenAI, Anthropic, AWS Bedrock, Google Vertex, Cerebras, Azure, Cohere, Mistral, Ollama, Groq, and more
- **[Drop-in Replacement](https://meridian.neria.cloud/features/drop-in-replacement)** - Replace OpenAI/Anthropic/GenAI SDKs with zero code changes
- **[Automatic Fallbacks](https://meridian.neria.cloud/features/fallbacks)** - Seamless failover between providers and models
- **[Streaming Support](https://meridian.neria.cloud/quickstart/gateway/streaming)** - Real-time response streaming for all providers

### Advanced Features

- **[Model Context Protocol (MCP)](https://meridian.neria.cloud/features/mcp)** - Enable AI models to use external tools (filesystem, web search, databases)
- **[Semantic Caching](https://meridian.neria.cloud/features/semantic-caching)** - Intelligent response caching based on semantic similarity
- **[Load Balancing](https://meridian.neria.cloud/features/fallbacks)** - Distribute requests across multiple API keys and providers
- **[Governance & Budget Management](https://meridian.neria.cloud/features/governance)** - Usage tracking, rate limiting, and cost control
- **[Custom Plugins](https://meridian.neria.cloud/enterprise/custom-plugins)** - Extensible middleware for analytics, monitoring, and custom logic

### Enterprise Features

- **[Clustering](https://meridian.neria.cloud/enterprise/clustering)** - Multi-node deployment with shared state
- **[User Provisioning (OIDC)](https://meridian.neria.cloud/enterprise/user-provisioning)** - OAuth 2.0 / OIDC login with background directory sync
- **[Vault Support](https://meridian.neria.cloud/enterprise/vault-support)** - Secure API key management
- **[Custom Analytics](https://meridian.neria.cloud/features/observability)** - Detailed usage insights and monitoring
- **[In-VPC Deployments](https://meridian.neria.cloud/enterprise/invpc-deployments)** - Private cloud deployment options

**Learn More**: [Complete Feature Documentation](https://meridian.neria.cloud/features/unified-interface)

---

## SDK Integrations

Replace your existing SDK base URLs to unlock Bifrost's features instantly:

### OpenAI SDK

```python
import openai
client = openai.OpenAI(
    base_url="http://localhost:8080/openai",
    api_key="dummy"  # Handled by Bifrost
)
```

### Anthropic SDK

```python
import anthropic
client = anthropic.Anthropic(
    base_url="http://localhost:8080/anthropic",
    api_key="dummy"  # Handled by Bifrost
)
```

### Google GenAI SDK

```python
import google.generativeai as genai
genai.configure(
    transport="rest",
    api_endpoint="http://localhost:8080/genai",
    api_key="dummy"  # Handled by Bifrost
)
```

**Complete Integration Guides**: [SDK Integrations](https://meridian.neria.cloud/integrations/what-is-an-integration)

---

## Documentation

### Getting Started

- [Quick Setup Guide](https://meridian.neria.cloud/quickstart/gateway/setting-up) - Detailed installation and configuration
- [Provider Configuration](https://meridian.neria.cloud/quickstart/gateway/provider-configuration) - Connect multiple AI providers
- [Integration Guide](https://meridian.neria.cloud/quickstart/gateway/integrations) - SDK replacements

### Advanced Topics

- [MCP Tool Calling](https://meridian.neria.cloud/features/mcp) - External tool integration
- [Semantic Caching](https://meridian.neria.cloud/features/semantic-caching) - Intelligent response caching
- [Fallbacks & Load Balancing](https://meridian.neria.cloud/features/fallbacks) - Reliability and scaling
- [Budget Management](https://meridian.neria.cloud/features/governance) - Cost control and governance

**Browse All Documentation**: [https://meridian.neria.cloud](https://meridian.neria.cloud)

---

*Built with ❤️ by [Maxim](https://getmaxim.ai)*

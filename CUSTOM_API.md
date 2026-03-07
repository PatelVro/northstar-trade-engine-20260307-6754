#  AI API 

## 

 AegisTrade  OpenAI  API
- OpenAI  API (gpt-4o, gpt-4-turbo )
- OpenRouter ()
-  (Ollama, LM Studio )
-  OpenAI  API 

## 

 `config.json`  API  trader

```json
{
  "traders": [
    {
      "id": "trader_custom",
      "name": "My Custom AI Trader",
      "ai_model": "custom",
      "exchange": "binance",

      "binance_api_key": "your_binance_api_key",
      "binance_secret_key": "your_binance_secret_key",

      "custom_api_url": "https://api.openai.com/v1",
      "custom_api_key": "sk-your-openai-api-key",
      "custom_model_name": "gpt-4o",

      "initial_balance": 1000,
      "scan_interval_minutes": 3
    }
  ]
}
```

## 

|  |  |  |  |
|-----|------|------|------|
| `ai_model` | string |  |  `"custom"`  API |
| `custom_api_url` | string |  | API  Base URL ( `/chat/completions`) `#`  URL |
| `custom_api_key` | string |  | API  |
| `custom_model_name` | string |  |  ( `gpt-4o`, `claude-3-5-sonnet` ) |

## 

### 1. OpenAI  API

```json
{
  "ai_model": "custom",
  "custom_api_url": "https://api.openai.com/v1",
  "custom_api_key": "sk-proj-xxxxx",
  "custom_model_name": "gpt-4o"
}
```

### 2. OpenRouter

```json
{
  "ai_model": "custom",
  "custom_api_url": "https://openrouter.ai/api/v1",
  "custom_api_key": "sk-or-xxxxx",
  "custom_model_name": "anthropic/claude-3.5-sonnet"
}
```

### 3.  Ollama

```json
{
  "ai_model": "custom",
  "custom_api_url": "http://localhost:11434/v1",
  "custom_api_key": "ollama",
  "custom_model_name": "llama3.1:70b"
}
```

### 4. Azure OpenAI

```json
{
  "ai_model": "custom",
  "custom_api_url": "https://your-resource.openai.azure.com/openai/deployments/your-deployment",
  "custom_api_key": "your-azure-api-key",
  "custom_model_name": "gpt-4"
}
```

### 5.  #

 API  `/chat/completions`  URL  `#`  URL

```json
{
  "ai_model": "custom",
  "custom_api_url": "https://api.example.com/v2/ai/chat/completions#",
  "custom_api_key": "your-api-key",
  "custom_model_name": "custom-model"
}
```

****`#`  `https://api.example.com/v2/ai/chat/completions`

## 

 API 
1.  OpenAI Chat Completions 
2.  `POST`  `/chat/completions`  URL  `#` 
3.  `Authorization: Bearer {api_key}` 
4.  OpenAI 

## 

1. **URL **`custom_api_url`  Base URL `/chat/completions`
   -  `https://api.openai.com/v1`
   -  `https://api.openai.com/v1/chat/completions`
   -  **** `/chat/completions` URL  `#`
     - `https://api.example.com/custom/path/chat/completions#`
     -  `#`  URL

2. **** `custom_model_name`  API 

3. **API ** API 

4. **** 120 

##  AI 

 AI  trader 

```json
{
  "traders": [
    {
      "id": "deepseek_trader",
      "ai_model": "deepseek",
      "deepseek_key": "sk-xxxxx",
      ...
    },
    {
      "id": "gpt4_trader",
      "ai_model": "custom",
      "custom_api_url": "https://api.openai.com/v1",
      "custom_api_key": "sk-xxxxx",
      "custom_model_name": "gpt-4o",
      ...
    },
    {
      "id": "claude_trader",
      "ai_model": "custom",
      "custom_api_url": "https://openrouter.ai/api/v1",
      "custom_api_key": "sk-or-xxxxx",
      "custom_model_name": "anthropic/claude-3.5-sonnet",
      ...
    }
  ]
}
```

## 

### 

****`APIcustom_api_url`

**** `ai_model: "custom"` 
- `custom_api_url`
- `custom_api_key`
- `custom_model_name`

### API 

****
1. URL 
   -  `/chat/completions`
   -  URL  `#`
2. API 
3. 
4. 

**** HTTP 

## 

 `deepseek`  `qwen` 

```json
{
  "ai_model": "deepseek",
  "deepseek_key": "sk-xxxxx"
}
```



```json
{
  "ai_model": "qwen",
  "qwen_key": "sk-xxxxx"
}
```

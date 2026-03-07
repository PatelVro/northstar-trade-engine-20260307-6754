#  Docker 

 Docker  AegisTrade AI 

##  



- **Docker**:  20.10 
- **Docker Compose**:  2.0 

###  Docker

> #### Docker Compose 
> 
> ****
> - ** Docker Desktop** Docker Compose
> - 
> -  macOSWindows Linux 
> 
> ****
> - ** docker-compose** Docker Compose 
> - ****Docker 20.10+  `docker compose` 
> -  `docker-compose`

#### macOS / Windows
 [Docker Desktop](https://www.docker.com/products/docker-desktop/)

****
```bash
docker --version
docker compose --version  # 
```

#### Linux (Ubuntu/Debian)
** Docker Desktop Docker CE**

```bash
#  Docker ( compose)
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

#  docker 
sudo usermod -aG docker $USER
newgrp docker

# 
docker --version
docker compose --version  # Docker 24+ 
```

##  3

###  1 

```bash
# 
cp config.json.example config.json

#  API 
nano config.json  # 
```

****
```json
{
  "traders": [
    {
      "id": "my_trader",
      "name": "My AI Trader",
      "ai_model": "deepseek",
      "binance_api_key": "YOUR_BINANCE_API_KEY",       //   API Key
      "binance_secret_key": "YOUR_BINANCE_SECRET_KEY", //   Secret Key
      "deepseek_key": "YOUR_DEEPSEEK_API_KEY",         //   DeepSeek API Key
      "initial_balance": 1000.0,
      "scan_interval_minutes": 3
    }
  ],
  "use_default_coins": true,
  "api_server_port": 8080
}
```

###  2 

```bash
# 
docker compose up -d --build

# 
docker compose up -d
```

****
- `--build`:  Docker 
- `-d`: detached mode

###  3 



- **Web **: http://localhost:3000
- **API **: http://localhost:8080/health

##  

### 
```bash
# 
docker compose ps

# 
docker compose ps --format json | jq
```

### 
```bash
# 
docker compose logs -f

# 
docker compose logs -f backend

# 
docker compose logs -f frontend

#  100 
docker compose logs --tail=100
```

### 
```bash
# 
docker compose stop

# 
docker compose down

# 
docker compose down -v
```

### 
```bash
# 
docker compose restart

# 
docker compose restart backend

# 
docker compose restart frontend
```

### 
```bash
# 
git pull

# 
docker compose up -d --build
```

##  

### 

 `docker-compose.yml`

```yaml
services:
  backend:
    ports:
      - "8080:8080"  #  ":8080"

  frontend:
    ports:
      - "3000:80"    #  ":80"
```

### 

 `docker-compose.yml` 

```yaml
services:
  backend:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

### 

 `.env` 

```bash
# .env
TZ=Asia/Shanghai
BACKEND_PORT=8080
FRONTEND_PORT=3000
```

 `docker-compose.yml` 

```yaml
services:
  backend:
    ports:
      - "${BACKEND_PORT}:8080"
```

##  



- `./decision_logs/`: AI 
- `./coin_pool_cache/`: 
- `./config.json`: 

****
```bash
# 
ls -la decision_logs/
ls -la coin_pool_cache/

# 
tar -czf backup_$(date +%Y%m%d).tar.gz decision_logs/ coin_pool_cache/ config.json

# 
tar -xzf backup_20241029.tar.gz
```

##  

### 

```bash
# 
docker compose logs backend
docker compose logs frontend

# 
docker compose ps -a

# 
docker compose build --no-cache
```

### 

```bash
# 
lsof -i :8080  # 
lsof -i :3000  # 

# 
kill -9 <PID>
```

### 

```bash
#  config.json 
ls -la config.json

# 
cp config.json.example config.json
```

### 

```bash
# 
docker inspect AegisTrade-backend | jq '.[0].State.Health'
docker inspect AegisTrade-frontend | jq '.[0].State.Health'

# 
curl http://localhost:8080/health
curl http://localhost:3000/health
```

### 

```bash
# 
docker compose exec frontend ping backend

# 
docker compose exec frontend wget -O- http://backend:8080/health
```

###  Docker 

```bash
# 
docker image prune -a

# 
docker volume prune

# 
docker system prune -a --volumes
```

##  

1. ** config.json  Git**
   ```bash
   #  config.json  .gitignore 
   echo "config.json" >> .gitignore
   ```

2. ****
   ```yaml
   # docker-compose.yml
   services:
     backend:
       environment:
         - BINANCE_API_KEY=${BINANCE_API_KEY}
         - BINANCE_SECRET_KEY=${BINANCE_SECRET_KEY}
   ```

3. ** API **
   ```yaml
   # 
   services:
     backend:
       ports:
         - "127.0.0.1:8080:8080"
   ```

4. ****
   ```bash
   docker compose pull
   docker compose up -d
   ```

##  

###  Nginx 

```nginx
# /etc/nginx/sites-available/AegisTrade
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/ {
        proxy_pass http://localhost:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

###  HTTPS (Let's Encrypt)

```bash
#  Certbot
sudo apt-get install certbot python3-certbot-nginx

#  SSL 
sudo certbot --nginx -d your-domain.com

# 
sudo certbot renew --dry-run
```

###  Docker Swarm ()

```bash
#  Swarm
docker swarm init

# 
docker stack deploy -c docker-compose.yml AegisTrade

# 
docker stack services AegisTrade

# 
docker service scale AegisTrade_backend=3
```

##  

### 

```bash
#  docker-compose.yml 
logging:
  driver: "json-file"
  options:
    max-size: "10m"
    max-file: "3"

# 
docker compose logs --timestamps | wc -l
```

### 

 Prometheus + Grafana 

```yaml
# docker-compose.yml ()
services:
  prometheus:
    image: prom/prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana
    ports:
      - "3001:3000"
```

##  

- **GitHub Issues**: [](https://github.com/yourusername/open-AegisTrade/issues)
- ****:  [README.md](README.md)
- ****:  Discord/Telegram 

##  

```bash
# 
docker compose up -d --build       # 
docker compose up -d               # 

# 
docker compose stop                # 
docker compose down                # 
docker compose down -v             # 

# 
docker compose ps                  # 
docker compose logs -f             # 
docker compose top                 # 

# 
docker compose restart             # 
docker compose restart backend     # 

# 
git pull && docker compose up -d --build

# 
docker compose down -v             # 
docker system prune -a             #  Docker 
```

---

  AegisTrade AI 

[](#-) Issue

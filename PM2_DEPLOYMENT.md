# Northstar Trading Bot - PM2

 PM2 

##  

### 1.  PM2

```bash
npm install -g pm2
```

### 2. 

```bash
./pm2.sh start
```



---

##  

### 

```bash
# 
./pm2.sh start

# 
./pm2.sh stop

# 
./pm2.sh restart

# 
./pm2.sh status

# 
./pm2.sh delete
```

### 

```bash
# 
./pm2.sh logs

# 
./pm2.sh logs backend

# 
./pm2.sh logs frontend
```

### 

```bash
# 
./pm2.sh build

# 
./pm2.sh rebuild
```

### 

```bash
#  PM2 CPU/
./pm2.sh monitor
```

---

##  



- ** Web **: http://localhost:3000
- ** API**: http://localhost:8080
- ****: http://localhost:8080/health

---

##  

### pm2.config.js

PM2 

```javascript
const path = require('path');

module.exports = {
  apps: [
    {
      name: 'northstar-backend',
      script: './northstar',           // Go 
      cwd: __dirname,             // 
      autorestart: true,
      max_memory_restart: '500M'
    },
    {
      name: 'northstar-frontend',
      script: 'npm',
      args: 'run dev',            // Vite 
      cwd: path.join(__dirname, 'web'), // 
      autorestart: true,
      max_memory_restart: '300M'
    }
  ]
};
```

****
```bash
./pm2.sh restart
```

---

##  

- ****: `./logs/backend-error.log`  `./logs/backend-out.log`
- ****: `./web/logs/frontend-error.log`  `./web/logs/frontend-out.log`

---

##  

 PM2 

```bash
# 1. 
./pm2.sh start

# 2. 
pm2 save

# 3. 
pm2 startup

# 4.  sudo
```

****
```bash
pm2 unstartup
```

---

##  

### 

****
```bash
./pm2.sh rebuild  # 
```

****
```bash
./pm2.sh restart  # Vite 
```

### 

```bash
./pm2.sh monitor
```

### 

```bash
pm2 info northstar-backend   # 
pm2 info northstar-frontend  # 
```

### 

```bash
pm2 flush
```

---

##  

### 

```bash
# 1. 
./pm2.sh logs

# 2. 
lsof -i :8080  # 
lsof -i :3000  # 

# 3. 
go build -o northstar
./northstar
```

### 

```bash
#  config.json 
ls -l config.json

# 
chmod +x northstar

# 
./northstar
```

### 

```bash
#  node_modules
cd web && npm install

# 
npm run dev
```

---

##  

### 1. 

 `pm2.config.js`

```javascript
{
  name: 'northstar-frontend',
  script: 'npm',
  args: 'run preview',  //  preview npm run build
  env: {
    NODE_ENV: 'production'
  }
}
```

### 2. 

```javascript
{
  name: 'northstar-backend',
  script: './northstar',
  instances: 2,  //  2 
  exec_mode: 'cluster'
}
```

### 3. 

```javascript
{
  autorestart: true,
  max_restarts: 10,
  min_uptime: '10s',
  max_memory_restart: '500M'
}
```

---

##   Docker 

|  | PM2  | Docker  |
|------|---------|------------|
|  |   |   |
|  |   |   |
|  |   |   |
|  | / | / |
|  |   |   |

****
- ****:  `./pm2.sh`
- ****:  `./start.sh` (Docker)

---

##  

```bash
./pm2.sh help
```

 PM2 https://pm2.keymetrics.io/

---

##  License

MIT

const path = require('path');

module.exports = {
  apps: [
    {
      name: 'AegisTrade-backend',
      script: './AegisTrade',
      cwd: __dirname, // 
      interpreter: 'none', // 
      instances: 1,
      autorestart: true,
      watch: false,
      max_memory_restart: '500M',
      env: {
        NODE_ENV: 'production'
      },
      error_file: './logs/backend-error.log',
      out_file: './logs/backend-out.log',
      log_date_format: 'YYYY-MM-DD HH:mm:ss Z',
      merge_logs: true
    },
    {
      name: 'AegisTrade-frontend',
      script: 'npm',
      args: 'run dev',
      cwd: path.join(__dirname, 'web'), //  web 
      instances: 1,
      autorestart: true,
      watch: false,
      max_memory_restart: '300M',
      env: {
        NODE_ENV: 'development',
        PORT: 3000
      },
      error_file: './logs/frontend-error.log',
      out_file: './logs/frontend-out.log',
      log_date_format: 'YYYY-MM-DD HH:mm:ss Z',
      merge_logs: true
    }
  ]
};

FROM node:20-alpine

WORKDIR /app

COPY package*.json ./

RUN npm install --legacy-peer-deps

COPY . .

EXPOSE 3000

# Set default API URL, can be overridden by environment variable
ENV REACT_APP_API_URL=http://localhost:8080

CMD ["sh", "-c", "npm start"]

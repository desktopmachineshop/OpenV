FROM node:20-alpine

WORKDIR /app

COPY package*.json ./

RUN npm install --legacy-peer-deps

COPY . .

# Accept API URL as build argument
ARG REACT_APP_API_URL=http://localhost:8080
ENV REACT_APP_API_URL=$REACT_APP_API_URL

EXPOSE 3000

CMD ["sh", "-c", "npm start"]

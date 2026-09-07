FROM node:20-alpine

WORKDIR /app

# Copy package manifests and install dependencies
COPY package.json package-lock.json* ./
RUN npm install --omit=dev

# Copy application assets, WASM engine, server, web and tests
COPY cordis.patch.yml ./
COPY src/ ./src/
COPY vendor/ ./vendor/
COPY public/ ./public/
COPY test/ ./test/

ENV PORT=3000
ENV HOST=0.0.0.0
EXPOSE 3000

CMD ["node", "src/server.js"]

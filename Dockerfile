FROM node:20-alpine

WORKDIR /app

# Copy package manifests and code
COPY package.json ./
COPY cordis.patch.yml ./
COPY src/ ./src/
COPY dist/ ./dist/
COPY scripts/ ./scripts/
COPY test/ ./test/

# Run build check
RUN node scripts/build.js

CMD ["node", "test/test-plugin.js"]

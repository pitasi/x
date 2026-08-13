FROM mcr.microsoft.com/playwright:v1.62.1-noble

WORKDIR /app
COPY server/package.json server/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY server/ ./

# settings are mounted, not baked in: -v <settings.json>:/settings.json
ENV CLIPPER_SETTINGS=/settings.json BROWSER_DATA_DIR=/data PORT=8090
EXPOSE 8090
CMD ["npx", "tsx", "src/main.ts"]

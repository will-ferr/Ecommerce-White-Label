FROM node:22.23.5-alpine AS build
WORKDIR /app
COPY . .
RUN npm ci --no-audit --no-fund

FROM build AS storefront-build
RUN npm run build -w @whitelabel/storefront

FROM node:22.23.5-alpine AS storefront
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3000
# The server only needs the Node runtime, so the package managers are removed.
RUN apk upgrade --no-cache \
 && rm -rf /usr/local/lib/node_modules /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack /opt/yarn-*
WORKDIR /app
COPY --from=storefront-build /app/apps/storefront/.output ./
USER node
EXPOSE 3000
CMD ["node", "server/index.mjs"]

FROM build AS panel-build
ARG APP
RUN npm run build -w "@whitelabel/${APP}" && mv "apps/${APP}/dist" /panel

FROM nginxinc/nginx-unprivileged:1.31.7-alpine AS panel
USER root
RUN apk upgrade --no-cache
USER 101
COPY panel.nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=panel-build /panel /usr/share/nginx/html
EXPOSE 8080

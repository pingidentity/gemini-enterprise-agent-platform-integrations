# Cloud Run is amd64; default the build to it (the Makefile also passes
# --platform) so a bare `docker build` can't ship arm64 off this Mac.
ARG PLATFORM=linux/amd64
FROM --platform=${PLATFORM} node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM --platform=${PLATFORM} nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]

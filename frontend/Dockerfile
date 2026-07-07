# syntax=docker/dockerfile:1

# ---- deps ----
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci

# ---- build ----
FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
# NEXT_PUBLIC_* are inlined at build time, so they must be build args.
ARG NEXT_PUBLIC_AUTH_PROVIDER=zitadel
ENV NEXT_PUBLIC_AUTH_PROVIDER=$NEXT_PUBLIC_AUTH_PROVIDER
ARG NEXT_PUBLIC_ZITADEL_ISSUER
ENV NEXT_PUBLIC_ZITADEL_ISSUER=$NEXT_PUBLIC_ZITADEL_ISSUER
ARG NEXT_PUBLIC_ZITADEL_CLIENT_ID
ENV NEXT_PUBLIC_ZITADEL_CLIENT_ID=$NEXT_PUBLIC_ZITADEL_CLIENT_ID
ARG NEXT_PUBLIC_ZITADEL_PROJECT_ID
ENV NEXT_PUBLIC_ZITADEL_PROJECT_ID=$NEXT_PUBLIC_ZITADEL_PROJECT_ID
ARG QUILL_API_BASE_URL=http://api:8080
ENV QUILL_API_BASE_URL=$QUILL_API_BASE_URL
# Sibling app URLs for the top-left app switcher — also client-inlined, so also
# build args (not runtime env). Quill's own URL isn't needed; it's always "current".
ARG NEXT_PUBLIC_ATLAS_URL
ENV NEXT_PUBLIC_ATLAS_URL=$NEXT_PUBLIC_ATLAS_URL
ARG NEXT_PUBLIC_TEMPO_URL
ENV NEXT_PUBLIC_TEMPO_URL=$NEXT_PUBLIC_TEMPO_URL
ARG NEXT_PUBLIC_FORGE_URL
ENV NEXT_PUBLIC_FORGE_URL=$NEXT_PUBLIC_FORGE_URL
RUN npm run build

# ---- run ----
FROM node:22-alpine AS run
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3001
ENV HOSTNAME=0.0.0.0

COPY --from=build /app/public ./public
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static

EXPOSE 3001
USER node
CMD ["node", "server.js"]

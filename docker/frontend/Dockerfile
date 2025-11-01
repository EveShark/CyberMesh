# CyberMesh Frontend - Next.js Production Build
# Simplified approach using next start (more reliable than standalone)

# ============================================================
# Stage 1: Dependencies
# ============================================================
FROM node:20-alpine AS deps
WORKDIR /app

# Install dependencies
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

# ============================================================
# Stage 2: Builder
# ============================================================
FROM node:20-alpine AS builder
WORKDIR /app

# Build arguments for Next.js public env vars
ARG NEXT_PUBLIC_BACKEND_API_BASE
ARG NEXT_PUBLIC_AI_API_BASE

# Set as environment variables for Next.js build
ENV NEXT_PUBLIC_BACKEND_API_BASE=${NEXT_PUBLIC_BACKEND_API_BASE}
ENV NEXT_PUBLIC_AI_API_BASE=${NEXT_PUBLIC_AI_API_BASE}

# Copy dependencies
COPY --from=deps /app/node_modules ./node_modules

# Copy application code
COPY frontend/ ./

# Build Next.js for production
RUN npm run build \
  && npm prune --omit=dev \
  && rm -rf .next/cache

# ============================================================
# Stage 3: Runner (Production)
# ============================================================
FROM node:20-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production

# Install wget for health checks
RUN apk add --no-cache wget

# Create non-root user (uid 1001 to differentiate from backend)
RUN addgroup -g 1001 -S nodejs && \
    adduser -S nextjs -u 1001

# Copy necessary files for next start
COPY --from=builder --chown=nextjs:nodejs /app/.next ./.next
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/package.json ./package.json
COPY --from=builder --chown=nextjs:nodejs /app/node_modules ./node_modules

USER nextjs

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/ || exit 1

EXPOSE 3000

# Use npm start (simpler and more reliable)
CMD ["npm", "start"]

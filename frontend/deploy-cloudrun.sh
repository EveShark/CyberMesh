#!/bin/bash
# Deploy Frontend to Google Cloud Run

PROJECT_ID="your-gcp-project-id"
REGION="us-central1"
SERVICE_NAME="cybermesh-frontend"
IMAGE_NAME="gcr.io/${PROJECT_ID}/${SERVICE_NAME}"

echo "🏗️  Building Docker image..."
docker build -t ${IMAGE_NAME}:latest .

echo "📤 Pushing to Google Container Registry..."
docker push ${IMAGE_NAME}:latest

echo "🚀 Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
  --image ${IMAGE_NAME}:latest \
  --platform managed \
  --region ${REGION} \
  --allow-unauthenticated \
  --set-env-vars NEXT_PUBLIC_BACKEND_API_BASE=https://your-backend-url.com/api/v1,NEXT_PUBLIC_AI_API_BASE=https://your-ai-url.com \
  --max-instances 10 \
  --memory 512Mi \
  --cpu 1 \
  --port 3000

echo "✅ Frontend deployed!"
gcloud run services describe ${SERVICE_NAME} --region ${REGION} --format 'value(status.url)'

# Deploy the custom scheduler controller
DOCKERHUB_USERNAME="YOUR DOCKERHUB USERNAME"
IMAGE_NAME="YOUR IMAGE NAME"

echo "Deploying the custom scheduler controller..."
echo "---------------------------------------------------------------"
make docker-build docker-push IMG=$DOCKERHUB_USERNAME/$IMAGE_NAME:latest
make deploy IMG=$DOCKERHUB_USERNAME/$IMAGE_NAME:latest
echo "The custom scheduler controller has been deployed successfully."
echo "---------------------------------------------------------------"
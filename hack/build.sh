# Build the custom-scheduler-controller image
echo "Deploying the custom scheduler controller..."
echo "---------------------------------------------------------------"
DOCKERHUB_USERNAME="tarikkada"
IMAGE_NAME="custom-scheduler-controller"
make docker-build docker-push IMG=$DOCKERHUB_USERNAME/$IMAGE_NAME:latest
echo "---------------------------------------------------------------"
# Deploy the custom scheduler controller
echo "Deploying the custom scheduler controller..."
echo "---------------------------------------------------------------"
DOCKERHUB_USERNAME="tarikkada"
IMAGE_NAME="custom-scheduler-controller"
make deploy IMG=$DOCKERHUB_USERNAME/$IMAGE_NAME:latest
echo "The custom scheduler controller has been deployed successfully."
echo "---------------------------------------------------------------"
# This script is used to build the docker image and push it to the docker hub
# and then apply the deployment and config files to the kubernetes cluster.

# Define the dockerhub to your dockerhub username
DOCKERHUB_USERNAME="tarikkada"
# Define the image name
IMAGE_NAME="scheduler-simple-flask"

docker build -t $DOCKERHUB_USERNAME/$IMAGE_NAME:latest .

docker push $DOCKERHUB_USERNAME/$IMAGE_NAME:latest

kubectl apply -f ./deployment.yaml

kubectl apply -f ./config.yaml
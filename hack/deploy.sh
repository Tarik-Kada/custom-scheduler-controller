# Deploy the custom scheduler controller
echo "Deploying the custom scheduler controller..."
echo "---------------------------------------------------------------"
make docker-build docker-push IMG=tarikkada/custom-scheduler-controller:latest
make deploy IMG=tarikkada/custom-scheduler-controller:latest
echo "The custom scheduler controller has been deployed successfully."
echo "---------------------------------------------------------------"
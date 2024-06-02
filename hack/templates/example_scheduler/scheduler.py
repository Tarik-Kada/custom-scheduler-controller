from flask import Flask, jsonify, request
import sys
import time

app = Flask(__name__)

@app.route('/', methods=['POST'])
def podScheduler():
    # Start timer to measure total time to get to scheduling decision
    start = time.time()
    # Read the request body
    request_data = request.get_json()

    # Get all node names in the cluster:
    nodeInfo = request_data['clusterInfo']['nodes']
    nodes = [node['nodeName'] for node in nodeInfo if node['nodeStatus'] == 'Ready']

    # Read the Paramaters field from the request body
    param = request_data['parameters']
    # Get the worker number from the request body and map it on so that 3 goes to 3 and 4 goes to 1
    print(param, file=sys.stdout)
    # Print the custom metrics
    print(f"Custom Metrics: {request_data['metrics']}", file=sys.stdout)
    worker = int(param['worker']) % len(nodes) + 1
    print(f"Worker: {nodes[worker]}", file=sys.stdout)

    # Print the total time to get to scheduling decision
    print(f"Time to get to scheduling decision: {time.time() - start}", file=sys.stdout)
    sys.stdout.flush()
    return jsonify({"node": nodes[worker]})

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)

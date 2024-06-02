FROM python:3.9-slim

WORKDIR /app
COPY scheduler.py /app

RUN pip install flask

CMD ["python", "scheduler.py"]

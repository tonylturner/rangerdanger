FROM python:3.12-slim

# iproute2 for set-gateway.sh
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install Python dependencies
COPY services/opendss-sim/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY services/opendss-sim/main.py services/opendss-sim/circuit.py services/opendss-sim/models.py ./
COPY services/opendss-sim/dss/ ./dss/

# Copy gateway script
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
RUN chmod +x /usr/local/bin/set-gateway.sh

EXPOSE 8080

CMD ["sh", "-c", "set-gateway.sh && python -m uvicorn main:app --host 0.0.0.0 --port 8080"]

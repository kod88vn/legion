FROM python:3.12-slim
RUN pip install --no-cache-dir "anthropic>=1,<2" "pydantic>=2,<3"
COPY mastermind.py /app/mastermind.py
ENV PYTHONUNBUFFERED=1
CMD ["python", "/app/mastermind.py"]

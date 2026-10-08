FROM python:3.12-alpine
RUN apk add --no-cache git postgresql-client
WORKDIR /app
COPY fixture.py verify.py /app/
CMD ["python", "fixture.py"]

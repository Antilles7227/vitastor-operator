FROM python:3.10-bullseye

WORKDIR /app
RUN wget -q -O - https://vitastor.io/debian/pubkey | apt-key add - && \
    echo 'deb https://vitastor.io/debian bullseye main' >> /etc/apt/sources.list && \
    apt update && apt install -y vitastor lp-solve && apt clean

COPY . /app
RUN pip install -r requirements.txt

CMD [ "uvicorn", "--host", "0.0.0.0", "vitastor-agent:app" ]
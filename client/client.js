let socket;
let reconnectInterval = 3000; // 3 seconds
const maxReconnectInterval = 30000; // 30 seconds

function connect() {
    socket = new WebSocket("ws://localhost:8080/subscribe");

    socket.onopen = () => {
        console.log("WebSocket connection established");
        // Perform any actions upon successful connection
    };

    socket.onmessage = (event) => {
        const message = JSON.parse(event.data);
        handleMessage(message);
    };

    socket.onclose = (event) => {
        console.log("WebSocket connection closed:", event.reason);
        setTimeout(reconnect, reconnectInterval);
        reconnectInterval = Math.min(reconnectInterval * 2, maxReconnectInterval);
    };

    socket.onerror = (error) => {
        console.error("WebSocket error:", error);
    };
}

function reconnect() {
    console.log("Attempting to reconnect...");
    connect();
}

function sendMessage(topic, data) {
    if (socket && socket.readyState === WebSocket.OPEN) {
        const message = JSON.stringify({ topic, data });
        socket.send(message);
    } else {
        console.error("WebSocket is not open. Message not sent.");
    }
}

function handleMessage(message) {
    switch (message.topic) {
        case 'makingMove':
            updateGameBoard(message.data);
            break;
        // Handle other topics as needed
    }
}

// Call connect to initiate the WebSocket connection
connect();
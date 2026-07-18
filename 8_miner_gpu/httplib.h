#pragma once
#include <string>
#include <sstream>
#include <iostream>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#else
#include <sys/socket.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <netdb.h>
#endif

namespace httplib {

struct Response {
    int status;
    std::string body;
};

class Client {
private:
    std::string host_;
    int port_;

public:
    Client(const std::string& host, int port) : host_(host), port_(port) {
#ifdef _WIN32
        WSADATA wsaData;
        WSAStartup(MAKEWORD(2, 2), &wsaData);
#endif
    }

    ~Client() {
#ifdef _WIN32
        WSACleanup();
#endif
    }

    Response Get(const std::string& path) {
        return request("GET", path, "");
    }

    Response Post(const std::string& path, const std::string& body, const std::string& content_type = "application/json") {
        return request("POST", path, body, content_type);
    }

private:
    Response request(const std::string& method, const std::string& path, const std::string& body, const std::string& content_type = "") {
        Response resp = {-1, ""};
        
        // Resolve host
        struct addrinfo hints = {}, *res = nullptr;
        hints.ai_family = AF_INET;
        hints.ai_socktype = SOCK_STREAM;
        
        std::string port_str = std::to_string(port_);
        if (getaddrinfo(host_.c_str(), port_str.c_str(), &hints, &res) != 0) {
            return resp;
        }

#ifdef _WIN32
        SOCKET sock = socket(res->ai_family, res->ai_socktype, res->ai_protocol);
        if (sock == INVALID_SOCKET) {
            freeaddrinfo(res);
            return resp;
        }
#else
        int sock = socket(res->ai_family, res->ai_socktype, res->ai_protocol);
        if (sock < 0) {
            freeaddrinfo(res);
            return resp;
        }
#endif

        if (connect(sock, res->ai_addr, (int)res->ai_addrlen) != 0) {
#ifdef _WIN32
            closesocket(sock);
#else
            close(sock);
#endif
            freeaddrinfo(res);
            return resp;
        }
        freeaddrinfo(res);

        // Build HTTP Request
        std::ostringstream req;
        req << method << " " << path << " HTTP/1.1\r\n";
        req << "Host: " << host_ << ":" << port_ << "\r\n";
        req << "Connection: close\r\n";
        if (!body.empty()) {
            req << "Content-Type: " << content_type << "\r\n";
            req << "Content-Length: " << body.length() << "\r\n";
        }
        req << "\r\n";
        if (!body.empty()) {
            req << body;
        }

        std::string req_str = req.str();
        send(sock, req_str.c_str(), (int)req_str.length(), 0);

        // Receive Response
        std::string raw_resp;
        char buf[4096];
        int bytes_received;
        while ((bytes_received = recv(sock, buf, sizeof(buf), 0)) > 0) {
            raw_resp.append(buf, bytes_received);
        }

#ifdef _WIN32
        closesocket(sock);
#else
        close(sock);
#endif

        // Parse Response
        size_t header_end = raw_resp.find("\r\n\r\n");
        if (header_end == std::string::npos) return resp;

        std::string headers = raw_resp.substr(0, header_end);
        resp.body = raw_resp.substr(header_end + 4);

        std::istringstream h_stream(headers);
        std::string http_ver;
        h_stream >> http_ver >> resp.status;

        return resp;
    }
};

} // namespace httplib

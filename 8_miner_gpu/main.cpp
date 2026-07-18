#include <iostream>
#include <string>
#include <chrono>
#include <thread>
#include <vector>
#include <iomanip>
#include <memory>
#include <random>
#include "httplib.h"

// Forward declarations of the CUDA mining functions
extern "C" bool init_cuda_device();
extern "C" bool run_cuda_miner(
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
);

extern "C" bool init_yona_cuda_device();
extern "C" bool run_yona_cuda_miner(
    uint64_t height,
    const uint8_t* parent_hash,
    const uint8_t* merkle_root,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
);

// Helper to convert hex string to byte array
void hex_to_bytes(const std::string& hex_str, uint8_t* bytes) {
    for (size_t i = 0; i < hex_str.length(); i += 2) {
        std::string byte_string = hex_str.substr(i, 2);
        uint8_t byte = (uint8_t)strtol(byte_string.c_str(), nullptr, 16);
        bytes[i / 2] = byte;
    }
}

// Helper to extract string fields from JSON
std::string extract_string_field(const std::string& json, const std::string& field) {
    size_t pos = json.find("\"" + field + "\"");
    if (pos == std::string::npos) return "";
    size_t colon = json.find(":", pos);
    if (colon == std::string::npos) return "";
    size_t start = json.find("\"", colon);
    if (start == std::string::npos) return "";
    size_t end = json.find("\"", start + 1);
    if (end == std::string::npos) return "";
    return json.substr(start + 1, end - start - 1);
}

// Helper to extract numeric fields from JSON
uint64_t extract_number_field(const std::string& json, const std::string& field) {
    size_t pos = json.find("\"" + field + "\"");
    if (pos == std::string::npos) return 0;
    size_t colon = json.find(":", pos);
    if (colon == std::string::npos) return 0;
    
    size_t start = colon + 1;
    while (start < json.length() && (json[start] == ' ' || json[start] == '\t' || json[start] == '\r' || json[start] == '\n')) {
        start++;
    }
    size_t end = start;
    while (end < json.length() && json[end] >= '0' && json[end] <= '9') {
        end++;
    }
    if (start == end) return 0;
    return std::stoull(json.substr(start, end - start));
}

int main(int argc, char* argv[]) {
    // Kiem tra tham so yeu cau tro giup (help)
    if (argc >= 2 && (std::string(argv[1]) == "-h" || std::string(argv[1]) == "--help")) {
        std::cout << "Usage: " << argv[0] << " [NODE_IP] [RPC_PORT] [WALLET_ADDRESS]\n"
                  << "       " << argv[0] << " --check\n"
                  << "       " << argv[0] << " --help | -h\n\n"
                  << "Options:\n"
                  << "  NODE_IP        IP address of the YonaCode node/pool (default: 127.0.0.1)\n"
                  << "  RPC_PORT       RPC port of the YonaCode node/pool (default: 8080)\n"
                  << "  WALLET_ADDRESS Wallet address to mine to (enables pool mode)\n"
                  << "  --check        Verify CUDA device compatibility and initialize CUDA\n"
                  << "  --help, -h     Show this help message\n";
        return 0;
    }

    if (argc >= 2 && std::string(argv[1]) == "--check") {
        if (init_cuda_device()) {
            std::cout << "[CUDA-SUCCESS] CUDA is fully operational." << std::endl;
            return 0;
        } else {
            std::cerr << "[CUDA-ERROR] CUDA initialization failed." << std::endl;
            return 1;
        }
    }

    std::string node_ip = "127.0.0.1";
    int node_port = 8080;
    std::string wallet_address = "";

    if (argc == 2) {
        std::string arg1 = argv[1];
        if (arg1.rfind("0x", 0) == 0 || arg1.length() == 64) {
            wallet_address = arg1;
            const char* env_ip = std::getenv("YONA_POOL_IP");
            node_ip = env_ip ? env_ip : "110.172.28.103";
            node_port = 8080;
        } else {
            node_ip = arg1;
        }
    } else {
        if (argc >= 2) {
            node_ip = argv[1];
        }
        if (argc >= 3) {
            node_port = std::stoi(argv[2]);
        }
        if (argc >= 4) {
            wallet_address = argv[3];
        }
    }

    std::string getwork_path = "/api/v1/miner/getwork";
    std::string submitwork_path = "/api/v1/miner/submitwork";
    if (!wallet_address.empty()) {
        getwork_path = "/api/v1/pool/getwork?address=" + wallet_address;
        submitwork_path = "/api/v1/pool/submitwork";
    }

    std::cout << "=================================================" << std::endl;
    std::cout << "🚀 YonaCode C++ CUDA GPU Miner (Blake3 ASIC Resistant)" << std::endl;
    std::cout << "📡 Connecting to Node/Pool at " << node_ip << ":" << node_port << std::endl;
    if (!wallet_address.empty()) {
        std::cout << "🏦 Pool Mining Mode Active. Wallet Address: " << wallet_address << std::endl;
    }
    std::cout << "=================================================" << std::endl;

    httplib::Client client(node_ip, node_port);

    // Initialize CUDA device first to check compatibility
    if (!init_cuda_device() || !init_yona_cuda_device()) {
        std::cerr << "[GPU-MINER] ❌ Critical: CUDA device initialization failed. Exiting." << std::endl;
        return 1;
    }

    // Configuration for CUDA Execution
    const uint32_t THREADS_PER_BLOCK = 256;
    const uint32_t NUMBER_OF_BLOCKS = 32768; // 32768 blocks * 256 threads * 32 nonces = 268,435,456 nonces per run
    const uint64_t NONCES_PER_RUN = (uint64_t)THREADS_PER_BLOCK * NUMBER_OF_BLOCKS * 32;

    std::random_device rd;
    std::mt19937_64 gen(rd());
    uint64_t base_nonce = gen();
    uint64_t total_hashes = 0;
    uint64_t intensity = 100; // Mức công suất GPU (Stress level) từ 10% đến 100%
    auto last_hashrate_time = std::chrono::steady_clock::now();

    const char* env_local_port = std::getenv("YONA_LOCAL_RPC_PORT");
    std::string local_port = env_local_port ? env_local_port : "";
    std::unique_ptr<httplib::Client> local_client;
    if (!local_port.empty()) {
        local_client = std::make_unique<httplib::Client>("127.0.0.1", std::stoi(local_port));
    }

    while (true) {
        httplib::Response res = client.Get(getwork_path.c_str());
        if (res.status != 200) {
            if (res.status == 204) {
                std::cout << "[GPU-MINER] 💤 Node has no active block template. Waiting 2 seconds..." << std::endl;
            } else {
                std::cerr << "[GPU-MINER] ❌ Failed to fetch work (Status: " << res.status << "). Retrying in 2 seconds..." << std::endl;
            }
            std::this_thread::sleep_for(std::chrono::seconds(2));
            continue;
        }

        // Step 2: Parse Block Template fields
        std::string header_hash_hex = extract_string_field(res.body, "header_hash");
        std::string target_hex = extract_string_field(res.body, "target");
        uint64_t height = extract_number_field(res.body, "height");
        uint64_t session_id = extract_number_field(res.body, "session_id");
        uint64_t current_intensity = extract_number_field(res.body, "intensity");
        if (current_intensity > 0 && current_intensity <= 100) {
            intensity = current_intensity;
        }

        // Override intensity from local node if running alongside Web UI
        if (local_client) {
            auto local_res = local_client->Get("/api/v1/node/cpu");
            if (local_res.status == 200) {
                uint64_t updated_intensity = extract_number_field(local_res.body, "cpu_intensity");
                if (updated_intensity > 0 && updated_intensity <= 100) {
                    intensity = updated_intensity;
                }
            }
        }

        if (header_hash_hex.empty() || target_hex.empty()) {
            std::cerr << "[GPU-MINER] ⚠️ Received invalid block template from Node. Retrying..." << std::endl;
            std::this_thread::sleep_for(std::chrono::seconds(1));
            continue;
        }

        uint8_t header_hash[32];
        uint8_t target[32];
        hex_to_bytes(header_hash_hex, header_hash);
        hex_to_bytes(target_hex, target);

        uint8_t parent_hash[32] = {0};
        uint8_t merkle_root[32] = {0};
        if (height >= 38500) {
            std::string parent_hash_hex = extract_string_field(res.body, "parent_hash");
            std::string merkle_root_hex = extract_string_field(res.body, "merkle_root");
            if (parent_hash_hex.empty() || merkle_root_hex.empty()) {
                std::cerr << "[GPU-MINER] ⚠️ Mining Yona Hash at Height >= 38500 requires parent_hash and merkle_root, but received none. Retrying..." << std::endl;
                std::this_thread::sleep_for(std::chrono::seconds(1));
                continue;
            }
            hex_to_bytes(parent_hash_hex, parent_hash);
            hex_to_bytes(merkle_root_hex, merkle_root);
        }

        // Convert target from Big Endian to Little Endian for the CUDA kernel
        uint8_t target_le[32];
        for (int i = 0; i < 32; i++) {
            target_le[i] = target[31 - i];
        }

        std::cout << "[GPU-MINER] 🔨 Mining Block #" << height << " (Session ID: " << session_id << ", Stress Level: " << intensity << "%)" << std::endl;

        base_nonce = (((uint64_t)rand()) << 32) | rand(); // Randomize starting nonce
        auto work_start_time = std::chrono::steady_clock::now();
        int checks_counter = 0;

        while (true) {
            uint64_t found_nonce = 0;
            
            auto run_start = std::chrono::steady_clock::now();
            // Step 3: Run CUDA Miner Kernel
            bool success = false;
            if (height >= 38500) {
                success = run_yona_cuda_miner(
                    height,
                    parent_hash,
                    merkle_root,
                    target_le,
                    base_nonce,
                    THREADS_PER_BLOCK,
                    NUMBER_OF_BLOCKS,
                    &found_nonce
                );
            } else {
                success = run_cuda_miner(
                    header_hash,
                    target_le,
                    base_nonce,
                    THREADS_PER_BLOCK,
                    NUMBER_OF_BLOCKS,
                    &found_nonce
                );
            }
            auto run_end = std::chrono::steady_clock::now();
            auto run_time_us = std::chrono::duration_cast<std::chrono::microseconds>(run_end - run_start).count();

            total_hashes += NONCES_PER_RUN;
            base_nonce += NONCES_PER_RUN;
            checks_counter++;

            // Dynamic GPU Throttling: Nghỉ ngơi giữa các chu kỳ chạy CUDA dựa trên Stress Level
            if (intensity < 100) {
                if (run_time_us == 0) run_time_us = 1000; // mặc định 1ms nếu chạy quá nhanh
                uint64_t sleep_us = run_time_us * (100 - intensity) / intensity;
                if (sleep_us > 0) {
                    std::this_thread::sleep_for(std::chrono::microseconds(sleep_us));
                }
            }

            // Track and display hashrate
            auto now = std::chrono::steady_clock::now();
            auto duration = std::chrono::duration_cast<std::chrono::milliseconds>(now - last_hashrate_time).count();
            if (duration >= 2000) {
                double hashrate = (double)total_hashes / (duration / 1000.0) / 1000000.0;
                std::cout << "[GPU-MINER] ⚡ Hashrate: " << std::fixed << std::setprecision(2) << hashrate << " MH/s | Height: #" << height << std::endl;
                
                uint64_t hashrate_h_s = (uint64_t)((double)total_hashes / (duration / 1000.0));
                std::ostringstream hr_payload;
                if (!wallet_address.empty()) {
                    hr_payload << "{\"hashrate\":" << hashrate_h_s << ",\"address\":\"" << wallet_address << "\"}";
                } else {
                    hr_payload << "{\"hashrate\":" << hashrate_h_s << "}";
                }
                
                // Gửi hashrate lên Node để cập nhật giao diện Dashboard
                client.Post("/api/v1/miner/hashrate", hr_payload.str(), "application/json");

                total_hashes = 0;
                last_hashrate_time = now;
            }

            // Step 4: If nonce is found, submit it immediately!
            if (success) {
                std::cout << "[GPU-MINER] 🏆 Success! Found valid nonce: " << found_nonce << " for Block #" << height << std::endl;
                
                std::ostringstream json_payload;
                if (!wallet_address.empty()) {
                    json_payload << "{\"nonce\":" << found_nonce << ",\"session_id\":" << session_id << ",\"address\":\"" << wallet_address << "\"}";
                } else {
                    json_payload << "{\"nonce\":" << found_nonce << ",\"session_id\":" << session_id << "}";
                }
                
                httplib::Response submit_res = client.Post(submitwork_path.c_str(), json_payload.str(), "application/json");
                if (submit_res.status == 200) {
                    std::cout << "[GPU-MINER] ✅ Block/Share submitted successfully!" << std::endl;
                } else {
                    std::cerr << "[GPU-MINER] ❌ Failed to submit block/share (Status: " << submit_res.status << ")" << std::endl;
                }
                break;
            }

            // Step 5: Periodically check if the mining task or stress level has updated
            if (checks_counter >= 15) {
                checks_counter = 0;
                httplib::Response check_res = client.Get(getwork_path.c_str());
                if (check_res.status == 200) {
                    uint64_t active_session = extract_number_field(check_res.body, "session_id");
                    uint64_t updated_intensity = extract_number_field(check_res.body, "intensity");
                    if (updated_intensity > 0 && updated_intensity <= 100) {
                        intensity = updated_intensity;
                    }

                    // Override intensity from local node if running alongside Web UI
                    if (local_client) {
                        auto local_res = local_client->Get("/api/v1/node/cpu");
                        if (local_res.status == 200) {
                            uint64_t updated_intensity_local = extract_number_field(local_res.body, "cpu_intensity");
                            if (updated_intensity_local > 0 && updated_intensity_local <= 100) {
                                intensity = updated_intensity_local;
                            }
                        }
                    }
                    if (active_session != session_id) {
                        std::cout << "[GPU-MINER] 🔄 Node published new block template (New SID: " << active_session << "). Switching task..." << std::endl;
                        break;
                    }
                } else {
                    std::cout << "[GPU-MINER] 💤 Node template no longer available or paused (Status: " << check_res.status << "). Stopping active mining..." << std::endl;
                    break;
                }
            }
        }
    }

    return 0;
}

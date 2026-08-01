#include <iostream>
#include <string>
#include <chrono>
#include <thread>
#include <vector>
#include <iomanip>
#include <memory>
#include <random>
#include <sstream>
#include <atomic>
#include <mutex>
#include <algorithm>
#include <cuda_runtime.h>
#include "httplib.h"

// Forward declarations of CUDA multi-device mining functions
extern "C" bool init_cuda_device_id(int device_id);
extern "C" bool run_cuda_miner_on_device(
    int device_id,
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
);

extern "C" bool init_yona_cuda_device_id(int device_id);
extern "C" bool run_yona_cuda_miner_on_device(
    int device_id,
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
);


// Forward declarations of backward compatibility wrappers
extern "C" bool init_cuda_device();
extern "C" bool init_yona_cuda_device();

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

// Helper to parse comma-separated device list (e.g. "0,1,2")
std::vector<int> parse_device_list(const std::string& device_str, int max_devices) {
    std::vector<int> result;
    std::stringstream ss(device_str);
    std::string token;
    while (std::getline(ss, token, ',')) {
        try {
            int dev = std::stoi(token);
            if (dev >= 0 && dev < max_devices) {
                if (std::find(result.begin(), result.end(), dev) == result.end()) {
                    result.push_back(dev);
                }
            }
        } catch (...) {}
    }
    return result;
}

int main(int argc, char* argv[]) {
    // Check help flag
    if (argc >= 2 && (std::string(argv[1]) == "-h" || std::string(argv[1]) == "--help")) {
        std::cout << "Usage: " << argv[0] << " [NODE_IP] [RPC_PORT] [WALLET_ADDRESS] [--devices 0,1,2]\n"
                  << "       " << argv[0] << " --check\n"
                  << "       " << argv[0] << " --help | -h\n\n"
                  << "Options:\n"
                  << "  NODE_IP        IP address of the YonaCode node/pool (default: 127.0.0.1)\n"
                  << "  RPC_PORT       RPC port of the YonaCode node/pool (default: 8080)\n"
                  << "  WALLET_ADDRESS Wallet address to mine to (enables pool mode)\n"
                  << "  --devices list Comma-separated GPU device IDs (e.g. 0,1,2). Default: All GPUs\n"
                  << "  --check        Verify CUDA device compatibility\n"
                  << "  --help, -h     Show this help message\n";
        return 0;
    }

    int system_gpu_count = 0;
    cudaError_t err = cudaGetDeviceCount(&system_gpu_count);
    if (err != cudaSuccess || system_gpu_count <= 0) {
        std::cerr << "[CUDA-ERROR] ❌ No CUDA-capable NVIDIA GPUs detected on this system!" << std::endl;
        return 1;
    }

    if (argc >= 2 && std::string(argv[1]) == "--check") {
        std::cout << "[CUDA-CHECK] Found " << system_gpu_count << " CUDA GPU(s):" << std::endl;
        for (int i = 0; i < system_gpu_count; i++) {
            cudaDeviceProp prop;
            cudaGetDeviceProperties(&prop, i);
            std::cout << "  -> GPU #" << i << ": " << prop.name << " ("
                      << (prop.totalGlobalMem / (1024 * 1024)) << " MB VRAM, Compute "
                      << prop.major << "." << prop.minor << ")" << std::endl;
        }
        return 0;
    }

    std::string node_ip = "127.0.0.1";
    int node_port = 8080;
    std::string wallet_address = "";
    std::vector<int> target_gpus;

    // Parse command line arguments
    for (int i = 1; i < argc; i++) {
        std::string arg = argv[i];
        if (arg == "--devices" && i + 1 < argc) {
            target_gpus = parse_device_list(argv[i + 1], system_gpu_count);
            i++;
        } else if (i == 1) {
            if (arg.rfind("0x", 0) == 0 || arg.length() == 64) {
                wallet_address = arg;
                const char* env_ip = std::getenv("YONA_POOL_IP");
                node_ip = env_ip ? env_ip : "110.172.28.103";
                node_port = 8080;
            } else {
                node_ip = arg;
            }
        } else if (i == 2) {
            try { node_port = std::stoi(arg); } catch (...) {}
        } else if (i == 3) {
            if (arg.rfind("0x", 0) == 0 || arg.length() == 64) {
                wallet_address = arg;
            }
        }
    }

    // Check environment variable for devices if not passed in command line
    if (target_gpus.empty()) {
        const char* env_devs = std::getenv("YONA_DEVICES");
        if (env_devs) {
            target_gpus = parse_device_list(env_devs, system_gpu_count);
        }
    }

    // If still empty, default to ALL available GPUs
    if (target_gpus.empty()) {
        for (int i = 0; i < system_gpu_count; i++) {
            target_gpus.push_back(i);
        }
    }

    std::string getwork_path = "/api/v1/miner/getwork";
    std::string submitwork_path = "/api/v1/miner/submitwork";
    if (!wallet_address.empty()) {
        getwork_path = "/api/v1/pool/getwork?address=" + wallet_address;
        submitwork_path = "/api/v1/pool/submitwork";
    }

    std::cout << "=================================================" << std::endl;
    std::cout << "🚀 YonaCode Multi-GPU CUDA Miner (Blake3 ASIC Resistant)" << std::endl;
    std::cout << "📡 Connecting to Node/Pool at " << node_ip << ":" << node_port << std::endl;
    if (!wallet_address.empty()) {
        std::cout << "🏦 Pool Mining Mode Active. Wallet Address: " << wallet_address << std::endl;
    }
    std::cout << "🎮 Parallel GPU Devices Active (" << target_gpus.size() << "/" << system_gpu_count << "):" << std::endl;
    for (int dev_id : target_gpus) {
        cudaDeviceProp prop;
        cudaGetDeviceProperties(&prop, dev_id);
        std::cout << "   -> GPU #" << dev_id << ": " << prop.name << " ("
                  << (prop.totalGlobalMem / (1024 * 1024)) << " MB VRAM)" << std::endl;
    }
    std::cout << "=================================================" << std::endl;

    // Initialize all selected CUDA GPUs
    for (int dev_id : target_gpus) {
        if (!init_cuda_device_id(dev_id) || !init_yona_cuda_device_id(dev_id)) {
            std::cerr << "[GPU-MINER] ❌ Critical: Failed to initialize CUDA on GPU #" << dev_id << std::endl;
            return 1;
        }
    }

    httplib::Client client(node_ip, node_port);

    const uint32_t THREADS_PER_BLOCK = 256;
    const uint32_t NUMBER_OF_BLOCKS = 32768; // 32768 blocks * 256 threads * 32 nonces = 268,435,456 nonces per run
    const uint64_t NONCES_PER_RUN = (uint64_t)THREADS_PER_BLOCK * NUMBER_OF_BLOCKS * 32;

    std::random_device rd;
    std::mt19937_64 gen(rd());
    uint64_t global_base_nonce = gen();
    std::atomic<uint64_t> total_hashes(0);
    std::atomic<uint64_t> intensity(100);
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

        std::string header_hash_hex = extract_string_field(res.body, "header_hash");
        std::string target_hex = extract_string_field(res.body, "target");
        uint64_t height = extract_number_field(res.body, "height");
        uint64_t session_id = extract_number_field(res.body, "session_id");
        uint64_t current_intensity = extract_number_field(res.body, "intensity");
        if (current_intensity > 0 && current_intensity <= 100) {
            intensity.store(current_intensity);
        }

        if (local_client) {
            auto local_res = local_client->Get("/api/v1/node/cpu");
            if (local_res.status == 200) {
                uint64_t updated_intensity = extract_number_field(local_res.body, "cpu_intensity");
                if (updated_intensity > 0 && updated_intensity <= 100) {
                    intensity.store(updated_intensity);
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
        uint8_t difficulty[32] = {0};
        if (height >= 38500) {
            std::string parent_hash_hex = extract_string_field(res.body, "parent_hash");
            std::string merkle_root_hex = extract_string_field(res.body, "merkle_root");
            std::string difficulty_hex = extract_string_field(res.body, "difficulty");
            if (parent_hash_hex.empty() || merkle_root_hex.empty() || difficulty_hex.empty()) {
                std::cerr << "[GPU-MINER] ⚠️ Mining Yona Hash at Height >= 38500 requires parent_hash, merkle_root, and difficulty, but received none. Retrying..." << std::endl;
                std::this_thread::sleep_for(std::chrono::seconds(1));
                continue;
            }
            hex_to_bytes(parent_hash_hex, parent_hash);
            hex_to_bytes(merkle_root_hex, merkle_root);
            hex_to_bytes(difficulty_hex, difficulty);
        }

        uint8_t target_le[32];
        for (int i = 0; i < 32; i++) {
            target_le[i] = target[31 - i];
        }

        std::cout << "[MULTI-GPU-MINER] 🔨 Mining Block #" << height << " across " << target_gpus.size()
                  << " GPU(s) (Session ID: " << session_id << ", Stress Level: " << intensity.load() << "%)" << std::endl;

        global_base_nonce = (((uint64_t)rand()) << 32) | rand();
        
        std::atomic<bool> solution_found(false);
        std::atomic<uint64_t> winning_nonce(0);
        std::atomic<int> winning_gpu_id(-1);
        std::atomic<bool> stop_mining_task(false);

        // Spawn parallel thread for each GPU
        std::vector<std::thread> gpu_threads;
        for (size_t g_idx = 0; g_idx < target_gpus.size(); g_idx++) {
            int dev_id = target_gpus[g_idx];
            uint64_t thread_start_nonce = global_base_nonce + (uint64_t)g_idx * (NONCES_PER_RUN * 1000ULL);

            gpu_threads.emplace_back([&, dev_id, thread_start_nonce]() {
                uint64_t local_nonce_offset = thread_start_nonce;
                while (!stop_mining_task.load() && !solution_found.load()) {
                    uint64_t found_nonce = 0;
                    auto run_start = std::chrono::steady_clock::now();
                    bool success = false;

                    if (height >= 38500) {
                        success = run_yona_cuda_miner_on_device(
                            dev_id, header_hash, target_le,
                            local_nonce_offset, THREADS_PER_BLOCK, NUMBER_OF_BLOCKS, &found_nonce
                        );
                    } else {
                        success = run_cuda_miner_on_device(
                            dev_id, header_hash, target_le,
                            local_nonce_offset, THREADS_PER_BLOCK, NUMBER_OF_BLOCKS, &found_nonce
                        );
                    }
                    auto run_end = std::chrono::steady_clock::now();
                    auto run_time_us = std::chrono::duration_cast<std::chrono::microseconds>(run_end - run_start).count();

                    total_hashes.fetch_add(NONCES_PER_RUN);
                    local_nonce_offset += NONCES_PER_RUN * target_gpus.size();

                    if (success) {
                        winning_nonce.store(found_nonce);
                        winning_gpu_id.store(dev_id);
                        solution_found.store(true);
                        break;
                    }

                    uint64_t cur_int = intensity.load();
                    if (cur_int < 100) {
                        if (run_time_us == 0) run_time_us = 1000;
                        uint64_t sleep_us = run_time_us * (100 - cur_int) / cur_int;
                        if (sleep_us > 0) {
                            std::this_thread::sleep_for(std::chrono::microseconds(sleep_us));
                        }
                    }
                }
            });
        }

        // Monitoring and submission thread
        int checks_counter = 0;
        while (!solution_found.load() && !stop_mining_task.load()) {
            std::this_thread::sleep_for(std::chrono::milliseconds(500));
            checks_counter++;

            // Hashrate reporting every 2 seconds
            auto now = std::chrono::steady_clock::now();
            auto duration = std::chrono::duration_cast<std::chrono::milliseconds>(now - last_hashrate_time).count();
            if (duration >= 2000) {
                uint64_t current_total = total_hashes.exchange(0);
                double hashrate = (double)current_total / (duration / 1000.0) / 1000000.0;
                std::cout << "[MULTI-GPU-MINER] ⚡ Combined Hashrate: " << std::fixed << std::setprecision(2)
                          << hashrate << " MH/s (" << target_gpus.size() << " GPUs active) | Height: #" << height << std::endl;
                
                uint64_t hashrate_h_s = (uint64_t)((double)current_total / (duration / 1000.0));
                std::ostringstream hr_payload;
                if (!wallet_address.empty()) {
                    hr_payload << "{\"hashrate\":" << hashrate_h_s << ",\"address\":\"" << wallet_address << "\"}";
                } else {
                    hr_payload << "{\"hashrate\":" << hashrate_h_s << "}";
                }
                client.Post("/api/v1/miner/hashrate", hr_payload.str(), "application/json");

                last_hashrate_time = now;
            }

            // Periodic task update check (Polls every 1 second: 2 * 500ms)
            if (checks_counter >= 2) {
                checks_counter = 0;
                httplib::Response check_res = client.Get(getwork_path.c_str());
                if (check_res.status == 200) {
                    uint64_t active_session = extract_number_field(check_res.body, "session_id");
                    uint64_t updated_intensity = extract_number_field(check_res.body, "intensity");
                    if (updated_intensity > 0 && updated_intensity <= 100) {
                        intensity.store(updated_intensity);
                    }
                    if (active_session != session_id) {
                        std::cout << "[MULTI-GPU-MINER] 🔄 Node published new block template (New SID: " << active_session << "). Switching task..." << std::endl;
                        stop_mining_task.store(true);
                        break;
                    }
                } else {
                    std::cout << "[MULTI-GPU-MINER] 💤 Node template transition in progress (Status: " << check_res.status << "). Pausing current task..." << std::endl;
                    stop_mining_task.store(true);
                    break;
                }
            }
        }

        // Wait for all GPU threads to finish
        for (auto& th : gpu_threads) {
            if (th.joinable()) th.join();
        }

        // Submit block solution if found by any GPU
        if (solution_found.load()) {
            uint64_t found_nonce = winning_nonce.load();
            int dev_id = winning_gpu_id.load();
            std::cout << "[MULTI-GPU-MINER] 🏆 Success! GPU #" << dev_id << " found valid nonce: "
                      << found_nonce << " for Block #" << height << std::endl;

            std::ostringstream json_payload;
            if (!wallet_address.empty()) {
                json_payload << "{\"nonce\":" << found_nonce << ",\"session_id\":" << session_id << ",\"address\":\"" << wallet_address << "\"}";
            } else {
                json_payload << "{\"nonce\":" << found_nonce << ",\"session_id\":" << session_id << "}";
            }

            httplib::Response submit_res = client.Post(submitwork_path.c_str(), json_payload.str(), "application/json");
            if (submit_res.status == 200) {
                std::cout << "[MULTI-GPU-MINER] ✅ Block/Share submitted successfully!" << std::endl;
            } else {
                std::cerr << "[MULTI-GPU-MINER] ❌ Failed to submit block/share (Status: " << submit_res.status << ")" << std::endl;
            }
        }
    }

    return 0;
}

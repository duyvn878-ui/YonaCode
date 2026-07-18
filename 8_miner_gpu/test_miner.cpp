#include <iostream>
#include <string>
#include <cstdint>
#include <cassert>

// Forward declaration of CUDA functions
extern "C" bool run_cuda_miner(
    const uint8_t* header_hash,
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

void test_easy_difficulty() {
    std::cout << "[TEST] Running Easy Difficulty Test..." << std::endl;
    
    uint8_t header_hash[32];
    for (int i = 0; i < 32; i++) header_hash[i] = 0x01; // Dummy header

    // Target = Max U256 (everything is accepted)
    uint8_t target[32];
    for (int i = 0; i < 32; i++) target[i] = 0xff;

    uint64_t found_nonce = 0;
    bool success = run_cuda_miner(header_hash, target, 10000, 256, 16, &found_nonce);
    
    assert(success == true);
    assert(found_nonce >= 10000);
    
    std::cout << "[TEST] Easy Difficulty Test Passed! Found Nonce: " << found_nonce << std::endl;
}

void test_impossible_difficulty() {
    std::cout << "[TEST] Running Impossible Difficulty Test..." << std::endl;
    
    uint8_t header_hash[32];
    for (int i = 0; i < 32; i++) header_hash[i] = 0x01;

    // Target = 0 (nothing is accepted)
    uint8_t target[32];
    for (int i = 0; i < 32; i++) target[i] = 0x00;

    uint64_t found_nonce = 0;
    bool success = run_cuda_miner(header_hash, target, 5000, 256, 16, &found_nonce);
    
    assert(success == false);
    
    std::cout << "[TEST] Impossible Difficulty Test Passed! (No false positive solution found)" << std::endl;
}

int main() {
    std::cout << "===========================================" << std::endl;
    std::cout << "🧪 YonaCode GPU Miner CUDA Unit Test" << std::endl;
    std::cout << "===========================================" << std::endl;

    try {
        test_easy_difficulty();
        test_impossible_difficulty();
        std::cout << "\n✅ ALL UNIT TESTS PASSED SUCCESSFULLY!" << std::endl;
    } catch (const std::exception& e) {
        std::cerr << "\n❌ UNIT TEST FAILED: " << e.what() << std::endl;
        return 1;
    }

    return 0;
}

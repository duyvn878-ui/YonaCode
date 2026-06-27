fn main() {
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path().unwrap());
    
    let proto_dir = std::path::Path::new("src/proto");
    if !proto_dir.exists() {
        std::fs::create_dir_all(proto_dir).unwrap();
    }

    let mut config = prost_build::Config::new();
    config.out_dir("src/proto");
    config.compile_protos(
        &[
            "../1_proto_defs/network/network.proto",
            "../1_proto_defs/common/common.proto",
            "../1_proto_defs/transaction/transaction.proto",
            "../1_proto_defs/block/block.proto",
        ],
        &["../1_proto_defs/"],
    ).unwrap();

    // Sinh mã gRPC cho SCL Service
    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .out_dir("src/proto")
        .compile(
            &["../1_proto_defs/scl/scl_service.proto"],
            &["../1_proto_defs/"],
        ).unwrap();

    // Sinh mã gRPC client & server cho Consensus/Miner Gateway để genz_miner.exe kết nối tới Go Node
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .out_dir("src/proto")
        .compile(
            &[
                "../1_proto_defs/consensus/consensus.proto",
                "../1_proto_defs/consensus/miner.proto"
            ],
            &["../1_proto_defs/"],
        ).unwrap();

    // [Audit V11.0 FIX] Rename btc_genz.*.rs to *.rs
    for entry in std::fs::read_dir(proto_dir).unwrap() {
        let entry = entry.unwrap();
        let path = entry.path();
        if let Some(filename) = path.file_name().and_then(|s| s.to_str()) {
            if filename.starts_with("btc_genz.") {
                let new_filename = filename.replace("btc_genz.", "");
                let new_path = proto_dir.join(new_filename);
                if path != new_path {
                    let _ = std::fs::remove_file(&new_path); 
                    std::fs::rename(path, new_path).unwrap();
                }
            }
        }
    }
}

fn main() {
    let protoc_path = protoc_bin_vendored::protoc_bin_path().expect("Không tìm thấy protoc vendored");
    unsafe {
        std::env::set_var("PROTOC", protoc_path);
    }
    
    let manifest_dir = std::env::var("CARGO_MANIFEST_DIR").unwrap();
    println!("cargo:rustc-link-search=native={}/lib", manifest_dir);
    // println!("cargo:rustc-link-lib=OpenCL");

    let mut config = prost_build::Config::new();
    config.out_dir("src/proto");
    config.compile_protos(
        &[
            "../1_proto_defs/common/common.proto", 
            "../1_proto_defs/transaction/transaction.proto", 
            "../1_proto_defs/block/block.proto"
        ], 
        &["../1_proto_defs/"]
    ).unwrap();

    // Rename btc_genz.*.rs to *.rs
    let proto_dir = std::path::Path::new("src/proto");
    if !proto_dir.exists() { std::fs::create_dir_all(proto_dir).unwrap(); }
    for entry in std::fs::read_dir(proto_dir).unwrap() {
        let entry = entry.unwrap();
        let path = entry.path();
        if let Some(filename) = path.file_name().and_then(|s| s.to_str()) {
            if filename.starts_with("btc_genz.") {
                let new_filename = filename.replace("btc_genz.", "");
                let new_path = proto_dir.join(new_filename);
                let _ = std::fs::remove_file(&new_path);
                std::fs::rename(path, new_path).unwrap();
            }
        }
    }
}

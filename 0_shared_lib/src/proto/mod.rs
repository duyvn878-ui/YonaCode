// File sinh ra bởi prost-build
pub mod common { include!("common.rs"); }
pub mod transaction { include!("transaction.rs"); }
pub mod block { include!("block.rs"); }
pub mod network { include!("network.rs"); }
pub mod consensus { include!("consensus.rs"); }

// gRPC Service
pub mod scl {
    // Scl Service phụ thuộc vào các module trên
    use super::*;
    include!("scl.rs");
}

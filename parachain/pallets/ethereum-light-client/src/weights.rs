//! Autogenerated weights for ethereum_light_client
//!
//! THIS FILE WAS AUTO-GENERATED USING THE SUBSTRATE BENCHMARK CLI VERSION 4.0.0-dev
//! DATE: 2021-12-25, STEPS: `50`, REPEAT: 20, LOW RANGE: `[]`, HIGH RANGE: `[]`
//! EXECUTION: Some(Wasm), WASM-EXECUTION: Compiled, CHAIN: Some("spec.json"), DB CACHE: 128

// Executed Command:
// target/release/snowbridge
// benchmark
// --chain
// spec.json
// --execution
// wasm
// --wasm-execution
// compiled
// --pallet
// ethereum-light-client
// --extrinsic
// import_header
// --repeat
// 20
// --steps
// 50
// --output
// pallets/ethereum-light-client/src/weights.rs
// --template
// templates/module-weight-template.hbs


#![cfg_attr(rustfmt, rustfmt_skip)]
#![allow(unused_parens)]
#![allow(unused_imports)]

use frame_support::{traits::Get, weights::{Weight, constants::RocksDbWeight}};
use sp_std::marker::PhantomData;

/// Weight functions needed for ethereum_light_client.
pub trait WeightInfo {
	fn import_header() -> Weight;
}

/// Weights for ethereum_light_client using the Snowbridge node and recommended hardware.
pub struct SnowbridgeWeight<T>(PhantomData<T>);
impl<T: frame_system::Config> WeightInfo for SnowbridgeWeight<T> {
	fn import_header() -> Weight {
		Weight::from_ref_time(2_253_588_000 as u64)
			.saturating_add(T::DbWeight::get().reads(17))
			.saturating_add(T::DbWeight::get().writes(22))
	}
}

// For backwards compatibility and tests
impl WeightInfo for () {
	fn import_header() -> Weight {
		Weight::from_ref_time(2_253_588_000 as u64)
			.saturating_add(RocksDbWeight::get().reads(17))
			.saturating_add(RocksDbWeight::get().writes(22))
	}
}

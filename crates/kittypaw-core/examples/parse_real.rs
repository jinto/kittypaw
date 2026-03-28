fn main() {
    let home = std::env::var("HOME").unwrap();
    let dir = format!("{}/.kittypaw/packages", home);

    let mgr = kittypaw_core::package_manager::PackageManager::new(std::path::PathBuf::from(&dir));
    match mgr.list_installed() {
        Ok(pkgs) => {
            println!("list_installed: {} packages", pkgs.len());
            match serde_json::to_string(&pkgs) {
                Ok(json) => println!(
                    "JSON OK ({} bytes)\n{}",
                    json.len(),
                    &json[..200.min(json.len())]
                ),
                Err(e) => println!("JSON FAIL: {e}"),
            }
        }
        Err(e) => println!("list_installed FAIL: {e}"),
    }
}

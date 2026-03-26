/// Spike 0.2: macOS Seatbelt fork-child sandbox
///
/// Proves:
/// 1. Parent forks a child process
/// 2. Child applies Seatbelt (sandbox_init) — IRREVERSIBLE per-process
/// 3. Child runs QuickJS code
/// 4. Child attempts /etc/passwd read → blocked
/// 5. Child attempts network socket → blocked
/// 6. Child returns result to parent via pipe
/// 7. Parent remains unrestricted
use std::ffi::CString;
use std::io::{Read, Write};
use std::os::unix::io::FromRawFd;

use rquickjs::{Context, Runtime};

// Seatbelt profile: deny everything except process ops and sysctl reads.
// No file reads (outside nothing), no network.
const SEATBELT_PROFILE: &str = r#"
(version 1)
(deny default)
(allow process*)
(allow sysctl-read)
(allow signal)
(allow mach*)
(allow ipc*)
(deny network*)
"#;

extern "C" {
    fn sandbox_init(
        profile: *const libc::c_char,
        flags: u64,
        errorbuf: *mut *mut libc::c_char,
    ) -> libc::c_int;
    fn sandbox_free_error(errorbuf: *mut libc::c_char);
}

fn apply_seatbelt() -> Result<(), String> {
    let profile_cstr = CString::new(SEATBELT_PROFILE).unwrap();
    let mut errbuf: *mut libc::c_char = std::ptr::null_mut();
    let ret = unsafe { sandbox_init(profile_cstr.as_ptr(), 0, &mut errbuf) };
    if ret != 0 {
        let msg = if !errbuf.is_null() {
            let s = unsafe { std::ffi::CStr::from_ptr(errbuf) }
                .to_string_lossy()
                .to_string();
            unsafe { sandbox_free_error(errbuf) };
            s
        } else {
            format!("sandbox_init returned {}", ret)
        };
        return Err(msg);
    }
    Ok(())
}

fn run_child(write_fd: libc::c_int) {
    // Apply Seatbelt — irreversible
    let sandbox_result = apply_seatbelt();
    let mut output = serde_json::Map::new();

    output.insert(
        "sandbox_applied".to_string(),
        serde_json::Value::Bool(sandbox_result.is_ok()),
    );
    if let Err(ref e) = sandbox_result {
        output.insert(
            "sandbox_error".to_string(),
            serde_json::Value::String(e.clone()),
        );
    }

    // Test 1: Run QuickJS code
    let js_result = {
        let rt = Runtime::new().unwrap();
        let ctx = Context::full(&rt).unwrap();
        ctx.with(|ctx| {
            let val = ctx.eval::<String, _>(r#"
                let x = 40 + 2;
                "result=" + x + " hello_from_quickjs"
            "#);
            match val {
                Ok(s) => s,
                Err(e) => format!("JS_ERROR: {}", e),
            }
        })
    };
    output.insert(
        "quickjs_result".to_string(),
        serde_json::Value::String(js_result),
    );

    // Test 2: Attempt to read /etc/passwd → should be denied by Seatbelt
    let passwd_result = std::fs::read_to_string("/etc/passwd");
    output.insert(
        "passwd_read_allowed".to_string(),
        serde_json::Value::Bool(passwd_result.is_ok()),
    );
    output.insert(
        "passwd_error".to_string(),
        serde_json::Value::String(match passwd_result {
            Ok(_) => "SUCCESS (unexpected!)".to_string(),
            Err(e) => format!("BLOCKED: {}", e),
        }),
    );

    // Test 3: Attempt to connect a network socket → connect/bind should be denied by Seatbelt
    // Note: socket() syscall itself is not network-restricted on macOS Seatbelt;
    // network operations (connect, bind) are what get denied.
    let sock_fd = unsafe { libc::socket(libc::AF_INET, libc::SOCK_STREAM, 0) };
    let connect_blocked;
    let socket_note;
    if sock_fd < 0 {
        connect_blocked = true;
        socket_note = format!("socket() BLOCKED: {}", std::io::Error::last_os_error());
    } else {
        // Try to connect to 8.8.8.8:53
        let addr = libc::sockaddr_in {
            sin_len: std::mem::size_of::<libc::sockaddr_in>() as u8,
            sin_family: libc::AF_INET as libc::sa_family_t,
            sin_port: 53u16.to_be(),
            sin_addr: libc::in_addr { s_addr: u32::from_be_bytes([8, 8, 8, 8]).to_be() },
            sin_zero: [0; 8],
        };
        let ret = unsafe {
            libc::connect(
                sock_fd,
                &addr as *const libc::sockaddr_in as *const libc::sockaddr,
                std::mem::size_of::<libc::sockaddr_in>() as libc::socklen_t,
            )
        };
        if ret < 0 {
            let err = std::io::Error::last_os_error();
            // EPERM (1) = blocked by sandbox; ECONNREFUSED/ETIMEDOUT = sandbox allowed but remote refused
            connect_blocked = err.raw_os_error() == Some(libc::EPERM)
                || err.raw_os_error() == Some(libc::EACCES);
            socket_note = format!("connect() result={}: {}", ret, err);
        } else {
            connect_blocked = false;
            socket_note = "connect() SUCCEEDED (unexpected!)".to_string();
        }
        unsafe { libc::close(sock_fd) };
    }
    output.insert(
        "network_connect_blocked".to_string(),
        serde_json::Value::Bool(connect_blocked),
    );
    output.insert(
        "socket_note".to_string(),
        serde_json::Value::String(socket_note),
    );

    // Write JSON result to pipe
    let json = serde_json::to_string_pretty(&serde_json::Value::Object(output)).unwrap();
    let mut pipe_write = unsafe { std::fs::File::from_raw_fd(write_fd) };
    let _ = pipe_write.write_all(json.as_bytes());
    // pipe_write dropped → fd closed → parent sees EOF
}

fn run_parent(read_fd: libc::c_int) {
    // Parent remains unrestricted — verify it can read /etc/passwd
    let parent_passwd = std::fs::read_to_string("/etc/passwd")
        .map(|s| format!("OK ({} bytes)", s.len()))
        .unwrap_or_else(|e| format!("FAILED: {}", e));

    // Read child result from pipe
    let mut pipe_read = unsafe { std::fs::File::from_raw_fd(read_fd) };
    let mut child_output = String::new();
    pipe_read.read_to_string(&mut child_output).unwrap();

    println!("=== Spike 0.2: Seatbelt Fork-Child Sandbox ===\n");
    println!("PARENT (unrestricted):");
    println!("  /etc/passwd read: {}\n", parent_passwd);

    println!("CHILD (Seatbelt-sandboxed):");
    match serde_json::from_str::<serde_json::Value>(&child_output) {
        Ok(v) => println!("{}", serde_json::to_string_pretty(&v).unwrap()),
        Err(_) => println!("{}", child_output),
    }

    println!("\n=== SUMMARY ===");
    if let Ok(v) = serde_json::from_str::<serde_json::Value>(&child_output) {
        let sandbox_ok = v["sandbox_applied"].as_bool().unwrap_or(false);
        let js_ok = v["quickjs_result"]
            .as_str()
            .map(|s| s.contains("result=42"))
            .unwrap_or(false);
        let passwd_blocked = !v["passwd_read_allowed"].as_bool().unwrap_or(true);
        let socket_blocked = v["network_connect_blocked"].as_bool().unwrap_or(false);

        println!(
            "  [{}] Seatbelt applied",
            if sandbox_ok { "PASS" } else { "FAIL" }
        );
        println!(
            "  [{}] QuickJS executed JS (result=42)",
            if js_ok { "PASS" } else { "FAIL" }
        );
        println!(
            "  [{}] /etc/passwd blocked in child",
            if passwd_blocked { "PASS" } else { "FAIL" }
        );
        println!(
            "  [{}] Network connect() blocked in child",
            if socket_blocked { "PASS" } else { "FAIL" }
        );
        println!(
            "  [PASS] Parent /etc/passwd read: {}",
            parent_passwd
        );

        if sandbox_ok && js_ok && passwd_blocked && socket_blocked {
            println!("\n  ALL CHECKS PASSED — Seatbelt fork-child model WORKS");
            std::process::exit(0);
        } else {
            println!("\n  SOME CHECKS FAILED");
            std::process::exit(1);
        }
    }
}

fn main() {
    // Create pipe for child→parent communication
    let mut fds = [0i32; 2];
    let ret = unsafe { libc::pipe(fds.as_mut_ptr()) };
    assert_eq!(ret, 0, "pipe() failed");
    let read_fd = fds[0];
    let write_fd = fds[1];

    let pid = unsafe { libc::fork() };
    match pid {
        -1 => {
            eprintln!("fork() failed: {}", std::io::Error::last_os_error());
            std::process::exit(1);
        }
        0 => {
            // Child: close read end
            unsafe { libc::close(read_fd) };
            run_child(write_fd);
            std::process::exit(0);
        }
        child_pid => {
            // Parent: close write end, wait for child
            unsafe { libc::close(write_fd) };

            // Read from pipe (child may still be writing)
            run_parent(read_fd);

            // Wait for child
            let mut status = 0i32;
            unsafe { libc::waitpid(child_pid, &mut status, 0) };
        }
    }
}

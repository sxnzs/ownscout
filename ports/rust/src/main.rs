mod cli;
mod contract;
mod evidence;
mod json;
mod ledger;
mod node;
mod packet;
mod result;
mod sha256;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let (output, code) = cli::run(&args);
    print!("{}", output);
    std::process::exit(code);
}

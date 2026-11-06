import Foundation

/// Fixed labels and numeric metadata only. Ordinary UDP payloads and addresses
/// never cross this diagnostic boundary; transaction IDs belong to marked probes.
public struct BridgeDiagnostic: Codable, Sendable {
    public enum Stage: String, Codable, Sendable {
        case socksReceive = "socks_receive", udpDial = "udp_dial", udpQueue = "udp_queue"
        case udpWriteStart = "udp_write_start", udpWrite = "udp_write"
        case pipeWriteStart = "pipe_write_start", pipeWrite = "pipe_write"
        case pipeRead = "pipe_read", stackDeliver = "stack_deliver"
        case udpRead = "udp_read", socksSend = "socks_send", status
    }
    public enum Outcome: String, Codable, Sendable { case ok, canceled, closed, timeout, ioError = "io_error", shortWrite = "short_write" }
    public let stage: Stage
    public let outcome: Outcome
    public let transaction: UInt64
    public let flow: UInt64
    public let packetKind: UInt16
    public let bytes: Int
    public let errno: Int
    public let timeMicros: Int64
    public let sequence: UInt64
    public let dropped: UInt64
    public let pid: Int

    public var isValid: Bool {
        let safe: UInt64 = (1 << 53) - 1
        return transaction <= safe && flow <= safe && sequence <= safe && dropped <= safe
            && [0, 1, 0x101, 0x111].contains(packetKind) && (0...65535).contains(bytes)
            && (0...4095).contains(errno) && timeMicros > 0 && pid > 1
    }
}

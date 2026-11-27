"""Obtain the runner's public IPv4 without forwarding credentials or using redirects."""
import http.client
import ipaddress
import os


def read(host, path):
    connection = http.client.HTTPSConnection(host, timeout=10)
    try:
        connection.request("GET", path, headers={"Accept-Encoding": "identity", "User-Agent": "evo-e2e-egress"})
        response = connection.getresponse()
        body = response.read(2049)
        if response.status != 200 or len(body) > 2048 or response.getheader("Content-Encoding", "identity") != "identity":
            raise RuntimeError("egress lookup unavailable")
        return body.decode("ascii")
    finally:
        connection.close()


def determine(fixed):
    if fixed:
        network = ipaddress.IPv4Network(fixed)
        if str(network) != fixed or network.prefixlen != 32:
            raise ValueError("invalid fixed egress")
        ip = network.network_address
    else:
        primary = ipaddress.IPv4Address(read("checkip.amazonaws.com", "/").strip())
        trace = read("api.ipify.org", "/").strip()
        if primary != ipaddress.IPv4Address(trace):
            raise ValueError("independent egress addresses differ")
        ip = primary
    if not ip.is_global or ip.is_multicast:
        raise ValueError("egress address is not public unicast")
    return f"{ip}/32"


if __name__ == "__main__":
    try:
        result = determine(os.environ.get("P90_RUNNER_CIDR", ""))
        with open(os.environ["GITHUB_ENV"], "a", encoding="ascii") as f:
            print("P90_RUNNER_CIDR=" + result, file=f)
    except Exception:
        raise SystemExit("Runner IPv4 could not be established. No rules opened.") from None

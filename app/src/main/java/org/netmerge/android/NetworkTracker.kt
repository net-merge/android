package org.netmerge.android

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Build
import java.net.Inet4Address
import java.net.Inet6Address

/**
 * Keeps Wi-Fi and mobile data both connected and reports the usable ones.
 *
 * Android normally tears mobile data down once Wi-Fi is up. Holding a [NetworkRequest] for each
 * transport keeps both alive for as long as the tracker runs (needs CHANGE_NETWORK_STATE).
 *
 * Each request carries a [NETWORK_TIMEOUT_MS] timeout so that [onUnavailable] fires promptly when
 * a transport cannot be brought up (e.g. no SIM, data disabled, carrier restriction), instead of
 * leaving the engine waiting in the STARTING phase indefinitely.
 */
class NetworkTracker(
    context: Context,
    private val onChange: (List<Tracked>) -> Unit,
) {
    data class Tracked(
        val name: String,
        val network: Network,
        val v4: Boolean,
        val v6: Boolean,
        /** The system's downstream bandwidth estimate in kbit/s, 0 if unknown. */
        val downKbps: Int,
    )

    private val cm = context.getSystemService(ConnectivityManager::class.java)
    private val lock = Any()
    private val links = LinkedHashMap<Network, Pair<Int, LinkProperties?>>() // network -> (transport, props)
    private val bandwidth = HashMap<Network, Int>() // network -> downstream kbit/s
    private val callbacks = mutableListOf<ConnectivityManager.NetworkCallback>()

    companion object {
        /**
         * How long to wait for a transport before giving up and calling [onUnavailable].
         * 20 seconds covers slow carrier bring-up without leaving the UI stuck indefinitely.
         */
        private const val NETWORK_TIMEOUT_MS = 20_000
    }

    fun start() {
        for (transport in listOf(NetworkCapabilities.TRANSPORT_WIFI, NetworkCapabilities.TRANSPORT_CELLULAR)) {
            val request = NetworkRequest.Builder()
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                .addTransportType(transport)
                .build()
            val cb = object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    synchronized(lock) { links[network] = transport to cm.getLinkProperties(network) }
                    publish()
                }

                override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) {
                    synchronized(lock) { links[network] = transport to lp }
                    publish()
                }

                override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                    val kbps = caps.linkDownstreamBandwidthKbps
                    val changed = synchronized(lock) {
                        val old = bandwidth.put(network, kbps)
                        // Ignore small wobbles; the estimate updates often.
                        old == null || kotlin.math.abs(kbps - old) > old / 4
                    }
                    if (changed) publish()
                }

                override fun onLost(network: Network) {
                    synchronized(lock) {
                        links.remove(network)
                        bandwidth.remove(network)
                    }
                    publish()
                }

                /**
                 * Called when the transport cannot be brought up within [NETWORK_TIMEOUT_MS].
                 * Publish the current (partial) list so the engine can proceed with whatever
                 * networks are available rather than hanging in the STARTING phase forever.
                 */
                override fun onUnavailable() {
                    publish()
                }
            }
            // requestNetwork (not registerNetworkCallback): this is what keeps the radio up.
            // The timeout triggers onUnavailable() if the transport can't be satisfied, e.g.
            // mobile data is disabled, no SIM, or the carrier rejects the request.
            cm.requestNetwork(request, cb, NETWORK_TIMEOUT_MS)
            callbacks += cb
        }
    }

    fun stop() {
        for (cb in callbacks) {
            runCatching { cm.unregisterNetworkCallback(cb) }
        }
        callbacks.clear()
        synchronized(lock) {
            links.clear()
            bandwidth.clear()
        }
    }

    fun current(): List<Tracked> {
        val (snapshot, bw) = synchronized(lock) { links.toList() to HashMap(bandwidth) }
        val seen = HashMap<String, Int>()
        return snapshot.map { (network, info) ->
            val (transport, lp) = info
            val base = if (transport == NetworkCapabilities.TRANSPORT_WIFI) "Wi-Fi" else "Mobile data"
            // Dual SIM or two Wi-Fi links get "Mobile data 2" etc.; names must stay unique and stable.
            val n = (seen[base] ?: 0) + 1
            seen[base] = n
            val name = if (n == 1) base else "$base $n"
            Tracked(name, network, hasV4(lp), hasV6(lp), bw[network] ?: 0)
        }.filter { it.v4 || it.v6 }
    }

    private fun publish() = onChange(current())

    private fun hasV4(lp: LinkProperties?): Boolean {
        if (lp == null) return false
        if (lp.linkAddresses.any { it.address is Inet4Address }) return true
        // IPv6-only mobile data with 464XLAT still carries IPv4 through the CLAT.
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.R && lp.nat64Prefix != null
    }

    private fun hasV6(lp: LinkProperties?): Boolean {
        if (lp == null) return false
        return lp.linkAddresses.any {
            val a = it.address
            // Accept any non-link-local, non-loopback global IPv6 address.
            // Note: we intentionally do NOT filter out ULA (fc00::/7) here. Many mobile
            // carriers — particularly in South/Southeast Asia (Jio, Airtel, Vi, AIS, True) —
            // assign addresses in the fc/fd prefix range that are fully routable within the
            // carrier's network and reach the public internet through NAT. Filtering them out
            // caused net-merge to believe cellular had no IPv6 at all, so the network was
            // discarded and merging silently fell back to Wi-Fi only.
            a is Inet6Address && !a.isLinkLocalAddress && !a.isLoopbackAddress
        }
    }
}

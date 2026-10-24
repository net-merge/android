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
            }
            // requestNetwork (not registerNetworkCallback): this is what keeps the radio up.
            cm.requestNetwork(request, cb)
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
            a is Inet6Address && !a.isLinkLocalAddress && !a.isLoopbackAddress &&
                (a.address[0].toInt() and 0xfe) != 0xfc // skip ULA (fc00::/7)
        }
    }
}

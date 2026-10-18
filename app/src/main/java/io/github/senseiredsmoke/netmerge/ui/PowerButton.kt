package io.github.senseiredsmoke.netmerge.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.github.senseiredsmoke.netmerge.Engine
import io.github.senseiredsmoke.netmerge.R

/**
 * The big on/off control. Off: a quiet grey ring. Starting: an amber arc chasing around.
 * On: a spinning Wi-Fi/mobile gradient ring with a breathing glow.
 */
@Composable
fun PowerButton(phase: Engine.Phase, onClick: () -> Unit, size: Dp = 196.dp) {
    val p = LocalPalette.current
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.93f else 1f, spring(dampingRatio = 0.45f), label = "press")

    val on = phase == Engine.Phase.ON
    val starting = phase == Engine.Phase.STARTING
    val active by animateFloatAsState(if (on) 1f else 0f, tween(700, easing = FastOutSlowInEasing), label = "active")
    val iconColor by animateColorAsState(
        when (phase) {
            Engine.Phase.ON -> p.good
            Engine.Phase.STARTING -> p.warn
            Engine.Phase.OFF -> p.muted
        },
        tween(500), label = "icon",
    )

    val t = rememberInfiniteTransition(label = "power")
    val spin by t.animateFloat(0f, 360f, infiniteRepeatable(tween(4000, easing = LinearEasing)), label = "spin")
    val chase by t.animateFloat(0f, 360f, infiniteRepeatable(tween(1100, easing = LinearEasing)), label = "chase")
    val breathe by t.animateFloat(0.55f, 1f, infiniteRepeatable(tween(1600, easing = FastOutSlowInEasing), RepeatMode.Reverse), label = "breathe")

    Box(
        modifier = Modifier
            .size(size)
            .scale(scale)
            .clip(CircleShape)
            .clickable(interactionSource = interaction, indication = null, role = Role.Switch, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(Modifier.fillMaxSize()) {
            val r = this.size.minDimension / 2
            val ring = r * 0.80f
            val stroke = r * 0.075f
            // Glow, only when on.
            if (active > 0f) {
                drawCircle(
                    Brush.radialGradient(
                        listOf(p.good.copy(alpha = 0.35f * active * breathe), Color.Transparent),
                        center = center, radius = r,
                    ),
                    radius = r,
                )
            }
            // Inner disc.
            drawCircle(p.card, radius = ring - stroke)
            // Base ring.
            drawCircle(p.muted.copy(alpha = 0.25f), radius = ring, style = Stroke(stroke))
            if (starting) {
                rotate(chase) {
                    drawArc(
                        p.warn, startAngle = 0f, sweepAngle = 90f, useCenter = false,
                        topLeft = Offset(center.x - ring, center.y - ring),
                        size = androidx.compose.ui.geometry.Size(ring * 2, ring * 2),
                        style = Stroke(stroke, cap = StrokeCap.Round),
                    )
                }
            }
            if (active > 0f) {
                rotate(spin) {
                    drawArc(
                        Brush.sweepGradient(listOf(p.wifi, p.good, p.cell, p.wifi), center),
                        startAngle = 0f, sweepAngle = 360f * active, useCenter = false,
                        topLeft = Offset(center.x - ring, center.y - ring),
                        size = androidx.compose.ui.geometry.Size(ring * 2, ring * 2),
                        style = Stroke(stroke, cap = StrokeCap.Round),
                    )
                }
            }
        }
        Icon(
            painter = painterResource(R.drawable.ic_power),
            contentDescription = if (on) "Turn off" else "Turn on",
            tint = iconColor,
            modifier = Modifier.size(size * 0.3f),
        )
    }
}

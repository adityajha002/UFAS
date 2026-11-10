package com.usict.attendance.security

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.spec.ECGenParameterSpec

object KeyManager {
    const val ALIAS = "attendance_device_key"

    private fun store() = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    fun hasKey(): Boolean = store().containsAlias(ALIAS)
    fun privateKey(): PrivateKey? = store().getKey(ALIAS, null) as? PrivateKey
    fun delete() { if (hasKey()) store().deleteEntry(ALIAS) }

    /** Creates a NEW EC P-256 signing key in the Keystore, replacing any old one. */
    fun generate() {
        delete()
        val isP = Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
        val attempts = buildList {
            if (isP) add(true to true)
            add(false to isP)
            if (isP) add(false to false)
        }

        var last: Exception? = null
        for ((strongBox, unlockedRequired) in attempts) {
            try {
                delete()
                build(strongBox = strongBox, unlockedRequired = unlockedRequired)
                return
            } catch (e: Exception) {
                last = e
                android.util.Log.w(
                    "KeyManager",
                    "keygen failed strongBox=$strongBox unlockedRequired=$unlockedRequired",
                    e,
                )
            }
        }

        throw IllegalStateException(
            "Key generation failed: ${last?.javaClass?.simpleName}: ${last?.message}",
            last,
        )
    }

    private fun build(strongBox: Boolean, unlockedRequired: Boolean) {
        val spec = KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .apply {
                if (strongBox) setIsStrongBoxBacked(true)
                if (unlockedRequired) setUnlockedDeviceRequired(true)
                // Deliberately no biometric or user-authentication requirement (device-only scope).
            }
            .build()
        KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore").run {
            initialize(spec)
            generateKeyPair()
        }
    }

    /** X.509 SubjectPublicKeyInfo DER. */
    fun publicKeyDer(): ByteArray = store().getCertificate(ALIAS).publicKey.encoded

    fun securityLevel(): String {
        val key = privateKey() ?: return "software"
        val info = KeyFactory.getInstance(key.algorithm, "AndroidKeyStore").getKeySpec(key, KeyInfo::class.java)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            when (info.securityLevel) {
                KeyProperties.SECURITY_LEVEL_STRONGBOX -> "strongbox"
                KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> "trusted_environment"
                else -> "software"
            }
        } else {
            @Suppress("DEPRECATION")
            if (info.isInsideSecureHardware) "trusted_environment" else "software"
        }
    }
}

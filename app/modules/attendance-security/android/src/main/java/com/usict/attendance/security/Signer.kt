package com.usict.attendance.security

import android.util.Base64
import java.security.Signature

object Signer {
    /** SHA256withECDSA, ASN.1 DER output, standard base64. */
    fun sign(message: ByteArray): String {
        val key = KeyManager.privateKey() ?: throw IllegalStateException("key_missing")
        val sig = Signature.getInstance("SHA256withECDSA").apply {
            initSign(key)
            update(message)
        }.sign()
        return Base64.encodeToString(sig, Base64.NO_WRAP)
    }
}

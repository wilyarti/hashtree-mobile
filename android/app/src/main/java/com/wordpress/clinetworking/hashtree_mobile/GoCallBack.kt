package com.wordpress.clinetworking.hashtree_mobile

import hashfunc.JavaCallback
import android.app.Activity
import kotlinx.android.synthetic.main.activity_main.*
import kotlinx.coroutines.experimental.CommonPool


class GoCallback(internal var context: Activity, internal var commoncontext: CommonPool) : JavaCallback {

    override final fun sendString(data: String) {
        try {
            // add a thread sleep or we will get random crashes
            // if the text being fed is too rapid
            var d: CharSequence
            if (!data.contains("\n")) {
                d = data + "\n"
            } else {
                d = data
            }
            val i = data.length
            println("TXT: $data : $i")

            context.runOnUiThread(java.lang.Runnable {
                context.textView.append(d)
            })

        } catch (e: Exception) {
            print("Error updating UI: ")
            println(e)
        }
    }
}
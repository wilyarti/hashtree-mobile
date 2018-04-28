package com.wordpress.clinetworking.hashtree_mobile


import hashfunc.JavaCallback

import android.app.Activity
import android.content.Context
import android.widget.TextView

class GoCallback(internal var context: Context) : JavaCallback {

    override fun sendString(data: String) {
        var txtView = (context as Activity).findViewById(R.id.textView) as TextView
//        txtView.append(data)
        println("TXT: $data")
    }
}
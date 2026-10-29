import Foundation
import CoreImage
let url=URL(fileURLWithPath:CommandLine.arguments[1])
do {
    let expected=try String(contentsOfFile:CommandLine.arguments[2],encoding:.utf8)
    guard let image=CIImage(contentsOf:url),let detector=CIDetector(ofType:CIDetectorTypeQRCode,context:CIContext(options:[.useSoftwareRenderer:true]),options:[CIDetectorAccuracy:CIDetectorAccuracyHigh]) else {throw NSError(domain:"QRCheck",code:1)}
    let features=detector.features(in:image).compactMap{$0 as? CIQRCodeFeature}
    guard features.count==1,features.first?.messageString==expected else {throw NSError(domain:"QRCheck",code:2,userInfo:[NSLocalizedDescriptionKey:"QR payload does not match the visible pairing URL"])}
    print("PASS: rendered QR decodes to the exact visible pairing URL")
} catch {fputs("QR validation failed: \(error.localizedDescription)\n",stderr);exit(1)}
